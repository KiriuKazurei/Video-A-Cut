package service_test

import (
	"errors"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// A ledger row exists only if its action committed. After a crash between the
// commit and the caller learning of it, the caller repeats the call with the
// versions it started with; by then the run has advanced, the auto version has
// moved and the auto run may even be revoked. The repeat must still report
// success and must not do or count anything again.
func TestAutoConfirmSceneReplayAfterCommitIsSuccess(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	first, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Stage == "scene_review" {
		t.Fatalf("run did not advance: %+v", first)
	}
	check := func(name string) {
		t.Helper()
		got, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
		if err != nil {
			t.Fatalf("%s: replay of a committed confirmation failed: %v", name, err)
		}
		if got.RunID != run.RunID || got.Version != first.Version {
			t.Fatalf("%s: replay returned %+v, want the run as the first call left it", name, got)
		}
		if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='scene_confirm'`, run.RunID); n != 1 {
			t.Fatalf("%s: ledger rows = %d", name, n)
		}
		if a, _ := env.svc.GetAuto(ctx, run.RunID); a.ActionsUsed != 1 {
			t.Fatalf("%s: actions_used = %d, a replay must not count", name, a.ActionsUsed)
		}
	}
	check("state advanced")
	now, _ := env.svc.GetAuto(ctx, run.RunID)
	if now.AutoVersion != view.AutoVersion+1 {
		t.Fatalf("auto_version = %d", now.AutoVersion)
	}
	if _, err := env.svc.RevokeAuto(ctx, "human:webui", run.RunID, "stop", now.AutoVersion, false); err != nil {
		t.Fatal(err)
	}
	check("revoked")
}

// Rule A has an early, side-effect-free check and an atomic one inside the
// start transaction. The hook flips an auto run to live in the window between
// them, so only the in-transaction check can refuse the start. The run is in
// ready_for_acceptance: the state in which idx_workflow_one_active does not
// protect the asset and Rule A is the only guard.
func TestRuleAInTransactionCheckRefusesAStartThatRacedTheEarlyCheck(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(env *autoEnv) error
	}{
		{"manual start", func(env *autoEnv) error {
			_, err := env.svc.StartWorkflow(t.Context(), "human:webui", "clip", "", "manual-race", "configured")
			return err
		}},
		{"second auto authorization", func(env *autoEnv) error {
			_, _, err := env.svc.StartAutoRun(t.Context(), "human:webui", env.request(t, "race-2", "", false))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newAutoEnv(t, false)
			run, _ := env.start(t, "")
			mustExec(t, storeOf(t, env.svc), `UPDATE workflow_runs SET status='ready_for_acceptance', stage='acceptance' WHERE run_id=?`, run.RunID)
			// Not live yet: the early check passes.
			mustExec(t, storeOf(t, env.svc), `UPDATE workflow_auto SET state='revoked' WHERE run_id=?`, run.RunID)
			fired := false
			env.svc.SetAfterEarlyAutoCheck(func() {
				fired = true
				mustExec(t, storeOf(t, env.svc), `UPDATE workflow_auto SET state='active' WHERE run_id=?`, run.RunID)
			})
			before := count(t, env.svc, `SELECT COUNT(*) FROM workflow_runs`)
			err := tc.start(env)
			if !fired {
				t.Fatal("the seam between the early and the in-transaction check was not reached")
			}
			if !errors.Is(err, model.ErrConflict) {
				t.Fatalf("start that raced the early check: %v", err)
			}
			if got := count(t, env.svc, `SELECT COUNT(*) FROM workflow_runs`); got != before {
				t.Fatalf("a refused start left %d new run(s)", got-before)
			}
		})
	}
}

// Auto-confirm may skip a scene a human already confirmed without applying the
// auto criteria to it: that scene is the human's. Scene 1 here has a confidence
// the auto-reviewer would refuse.
func TestAutoConfirmSceneSkipsHumanConfirmedSceneThatFailsAutoCriteria(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	run, view := env.atSceneReview(t, "", [2]float64{0.3, 0.9})
	afterHuman, err := env.svc.ConfirmScene(ctx, "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "confirmed", "looks right to me")
	if err != nil {
		t.Fatal(err)
	}
	after, err := env.svc.AutoConfirmScene(ctx, run.RunID, afterHuman.Version, view.AutoVersion, run.CurrentRevisionID)
	if err != nil {
		t.Fatalf("a human-confirmed scene must not be judged by the auto criteria: %v", err)
	}
	if after.Stage == "scene_review" {
		t.Fatalf("batch did not complete: %+v", after)
	}
	reviews, _ := storeOf(t, env.svc).ListSceneReviews(ctx, run.RunID, run.CurrentRevisionID)
	actors := map[string]string{}
	for _, r := range reviews {
		actors[r.SceneID] = r.Actor
	}
	if actors["scn_1"] != "human:webui" || actors["scn_2"] != model.AutoActor {
		t.Fatalf("actors = %+v", actors)
	}
	// The same scene left to the auto-reviewer is refused, so the skip is what let it through.
	env2 := newAutoEnv(t, false)
	run2, view2 := env2.atSceneReview(t, "", [2]float64{0.3, 0.9})
	if _, err := env2.svc.AutoConfirmScene(ctx, run2.RunID, run2.Version, view2.AutoVersion, run2.CurrentRevisionID); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("control: scene at 0.3 without a human decision: %v", err)
	}
}

// AutoConfirmScene only acts on a run that is at scene review AND waiting for
// the review. A run whose stage says scene_review but whose status does not,
// or the reverse, is refused with nothing written. The runs here have no
// active task, so ensureIdle has nothing to object to and cannot hide the check.
func TestAutoConfirmSceneRequiresStageAndAwaitingReviewStatus(t *testing.T) {
	for _, tc := range []struct{ name, set string }{
		{"stage is scene_review but status is running", `status='running'`},
		{"stage is scene_review but status is failed", `status='failed'`},
		{"status is awaiting_review but stage is draft_review", `stage='draft_review'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newAutoEnv(t, false)
			ctx := t.Context()
			run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
			mustExec(t, storeOf(t, env.svc), `UPDATE workflow_runs SET `+tc.set+` WHERE run_id=?`, run.RunID)
			_, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
			if !errors.Is(err, model.ErrInvalidState) {
				t.Fatalf("got %v, want ErrInvalidState", err)
			}
			if n := count(t, env.svc, `SELECT COUNT(*) FROM scene_reviews WHERE run_id=?`, run.RunID); n != 0 {
				t.Fatalf("%d scene review(s) written on a run that is not at the gate", n)
			}
			if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=?`, run.RunID); n != 0 {
				t.Fatalf("%d ledger row(s) written", n)
			}
		})
	}
}
