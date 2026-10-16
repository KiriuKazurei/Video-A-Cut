package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestScoreAutoRunPublishesAutoChangedOnceAndNotOnReplay(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	pid := e.preview(t, true)
	time.Sleep(50 * time.Millisecond) // the preview start announces itself too
	base := len(e.events.named("auto.changed", 0))
	if _, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1); err != nil {
		t.Fatal(err)
	}
	if got := len(e.events.named("auto.changed", base+1)); got != base+1 {
		t.Fatalf("scoring published %d auto.changed, want 1", got-base)
	}
	if _, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1); err != nil { // replay
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := len(e.events.named("auto.changed", base+2)); got != base+1 {
		t.Fatalf("a replay published again: %d", got-base)
	}
}

// The preview is read before the write lock is taken; the transaction must
// notice that it went stale in between and store nothing.
func TestScoreAutoRunRechecksThePreviewInsideTheTransaction(t *testing.T) {
	cases := map[string]func(e *scoreEnv, pid string){
		"invalidated": func(e *scoreEnv, pid string) {
			if err := e.st.InvalidatePreviewJob(t.Context(), pid); err != nil {
				t.Fatal(err)
			}
		},
		"status changed": func(e *scoreEnv, pid string) {
			job, _ := e.st.GetPreviewJob(t.Context(), pid)
			mustExec(t, e.st, `UPDATE tasks SET status='failed' WHERE task_id=?`, job.TaskID)
		},
	}
	for name, stale := range cases {
		t.Run(name, func(t *testing.T) {
			e := newScoreEnv(t, 0.9, "团战")
			pid := e.preview(t, true)
			before := e.auto(t)
			fired := false
			e.svc.SetAfterScoreCollect(func() {
				fired = true
				stale(e, pid)
			})
			if _, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, before.AutoVersion); !errors.Is(err, model.ErrConflict) {
				t.Fatalf("err = %v, want conflict", err)
			}
			if !fired {
				t.Fatal("the seam was not reached")
			}
			if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_reviews WHERE run_id=?`, e.run.RunID); n != 0 {
				t.Fatalf("a stale preview was scored: %d reviews", n)
			}
			if after := e.auto(t); after.ActionsUsed != before.ActionsUsed || after.AutoVersion != before.AutoVersion {
				t.Fatalf("a refused scoring changed the overlay: %+v", after)
			}
		})
	}
}

func TestAutoConfirmSceneReplaySuppressesEvents(t *testing.T) {
	env := newAutoEnv(t, false)
	bus := events.New()
	env.svc.SetBus(bus)
	rec := &recordedEvents{}
	bus.Subscribe("auto.changed", rec.add)
	bus.Subscribe("review.changed", rec.add)
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	if _, err := env.svc.AutoConfirmScene(t.Context(), run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID); err != nil {
		t.Fatal(err)
	}
	if len(rec.named("auto.changed", 1)) != 1 || len(rec.named("review.changed", 1)) != 1 {
		t.Fatal("the first confirmation must announce itself")
	}
	if _, err := env.svc.AutoConfirmScene(t.Context(), run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID); err != nil {
		t.Fatalf("replay: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if a, b := len(rec.named("auto.changed", 2)), len(rec.named("review.changed", 2)); a != 1 || b != 1 {
		t.Fatalf("a replay announced again: auto.changed=%d review.changed=%d", a, b)
	}
}

func TestAutoSceneConfidenceBoundaryIsInclusive(t *testing.T) {
	env := newAutoEnv(t, false)
	run, view := env.atSceneReview(t, "", [2]float64{0.60, 0.9})
	if _, err := env.svc.AutoConfirmScene(t.Context(), run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID); err != nil {
		t.Fatalf("0.60 must be confirmed: %v", err)
	}
	env2 := newAutoEnv(t, false)
	run2, view2 := env2.atSceneReview(t, "", [2]float64{0.59, 0.9})
	_, err := env2.svc.AutoConfirmScene(t.Context(), run2.RunID, run2.Version, view2.AutoVersion, run2.CurrentRevisionID)
	var pe *service.AutoPolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("0.59 must be refused by policy: %v", err)
	}
}
