package service_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/autoreview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// fakeClock is the service clock of the driver tests: it moves only when told.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Now().UTC()} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// setScope rewrites the stored scope and the mirrored cap columns.
func (e *scoreEnv) setScope(t *testing.T, f func(*model.AutoScope)) {
	t.Helper()
	a := e.auto(t)
	f(&a.Scope)
	body, _ := json.Marshal(a.Scope)
	mustExec(t, e.st, `UPDATE workflow_auto SET scope_json = ?, max_rounds = ?, max_total_rounds = ? WHERE run_id = ?`,
		string(body), a.Scope.MaxRounds, a.Scope.MaxTotalRounds, e.run.RunID)
}

func (e *scoreEnv) fullScope(t *testing.T) {
	t.Helper()
	e.setScope(t, func(s *model.AutoScope) {
		s.AutoSceneConfirm, s.AutoNarrationApprove, s.AutoPreviewExport = true, true, true
		s.MaxRounds, s.MaxTotalRounds, s.MaxStageRetries = 1, 5, 1
		s.MaxActions, s.MaxTTSChars, s.MaxWallSeconds, s.DecisionTimeoutSeconds = 100, 20000, 7200, 259200
	})
}

// scored scores the preview and returns the review and the overlay version.
func (e *scoreEnv) scored(t *testing.T, pid string) (model.AutoReview, int) {
	t.Helper()
	review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, e.auto(t).AutoVersion)
	if err != nil {
		t.Fatal(err)
	}
	return review, e.auto(t).AutoVersion
}

func (e *scoreEnv) route(t *testing.T, review model.AutoReview, version int) (model.WorkflowAuto, error) {
	t.Helper()
	return e.svc.AutoRouteReview(t.Context(), e.run.RunID, review.ReviewID, version)
}

func TestRouteHighCompletesAndReleasesHold(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	pid := e.preview(t, true)
	review, v := e.scored(t, pid)
	got, err := e.route(t, review, v)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.AutoStateCompleted || got.DeliveryHold || got.HaltReason != "" || got.AutoVersion != v+1 {
		t.Fatalf("overlay = %+v", got)
	}
	run, _ := e.st.GetWorkflow(t.Context(), e.run.RunID)
	if run.Status != model.WorkflowReadyAcceptance || run.Version != e.run.Version {
		t.Fatalf("HIGH changed the run: %+v", run)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='auto_route' AND target='high'`, e.run.RunID); n != 1 {
		t.Fatalf("route decisions = %d", n)
	}
	if len(e.events.named("auto.changed", 1)) == 0 {
		t.Fatal("no auto.changed")
	}
	// Routing again is refused: the run is no longer active.
	if _, err := e.route(t, review, v+1); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("second route: %v", err)
	}
}

func TestRouteMediumWaitsForTheUserWithTheClockPaused(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼") // scores MEDIUM (see TestScoreAutoRunLowConfidenceBlocksHigh)
	e.fullScope(t)
	clock := newFakeClock()
	e.svc.SetClock(clock.Now)
	pid := e.preview(t, true)
	review, v := e.scored(t, pid)
	if review.Band != autoreview.BandMedium {
		t.Fatalf("band = %s", review.Band)
	}
	got, err := e.route(t, review, v)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.AutoStateAwaitingDecision || got.HaltReason != model.AutoWaitMedium || !got.DeliveryHold ||
		got.PausedAt == nil || !got.PausedAt.Equal(clock.Now()) ||
		got.DecisionDeadlineAt == nil || !got.DecisionDeadlineAt.Equal(clock.Now().Add(259200*time.Second)) {
		t.Fatalf("overlay = %+v", got)
	}
}

func TestRouteLowImprovableStartsABoundedRerun(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	e.setThresholds(t, map[string]float64{autoreview.KeyHighScore: 0.99, autoreview.KeyMediumScore: 0.98})
	pid := e.preview(t, true)
	review, v := e.scored(t, pid)
	if review.Band != autoreview.BandLow {
		t.Fatalf("band = %s", review.Band)
	}
	before := e.auto(t)
	got, err := e.route(t, review, v)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.AutoStateActive || got.Round != 1 || got.TotalRounds != 1 || got.ActionsUsed != before.ActionsUsed+1 ||
		got.AutoVersion != v+1 || got.DeliveryHold {
		t.Fatalf("overlay = %+v", got)
	}
	run, _ := e.st.GetWorkflow(t.Context(), e.run.RunID)
	if run.Status != model.WorkflowRunning || run.Stage != model.TaskTypeRecognize || run.CurrentRevisionID != run.BaseRevisionID || run.Version != e.run.Version+1 {
		t.Fatalf("run = %+v", run)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM tasks WHERE asset_id=? AND type='recognize' AND status='queued'`, run.AssetID); n != 1 {
		t.Fatalf("queued recognize tasks = %d", n)
	}
	if asset, _ := e.st.GetAsset(t.Context(), run.AssetID); asset.Status != model.AssetStatusIngested {
		t.Fatalf("asset = %s", asset.Status)
	}
	job, _ := e.st.GetPreviewJob(t.Context(), pid)
	if !job.Invalidated {
		t.Fatal("the old preview was not invalidated")
	}
	view, _ := e.svc.GetAuto(t.Context(), e.run.RunID)
	if len(view.Rounds) != 1 || view.Rounds[0].Round != 1 || view.Rounds[0].BaseRevisionID != run.BaseRevisionID {
		t.Fatalf("rounds = %+v", view.Rounds)
	}
	var fb struct {
		Codes []model.AutoFeedbackItem `json:"codes"`
		Texts []string                 `json:"texts"`
		User  string                   `json:"user"`
	}
	if err := json.Unmarshal([]byte(view.Rounds[0].Feedback), &fb); err != nil || fb.User != "" {
		t.Fatalf("feedback = %s (%v)", view.Rounds[0].Feedback, err)
	}
	if len(fb.Codes) != len(fb.Texts) {
		t.Fatalf("every code needs its template text: %s", view.Rounds[0].Feedback)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='rerun'`, e.run.RunID); n != 1 {
		t.Fatalf("rerun ledger rows = %d", n)
	}
	if n, _ := e.st.CountAutoActions(t.Context(), e.run.RunID); n != got.ActionsUsed {
		t.Fatalf("ledger %d != actions_used %d", n, got.ActionsUsed)
	}
	// A repeat of the same route changes nothing.
	if _, err := e.route(t, review, v); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("repeat route: %v", err)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM workflow_auto_rounds WHERE run_id=?`, e.run.RunID); n != 1 {
		t.Fatalf("rounds after repeat = %d", n)
	}
}

func TestRouteLowCapsAndExhaustion(t *testing.T) {
	low := map[string]float64{autoreview.KeyHighScore: 0.99, autoreview.KeyMediumScore: 0.98}
	cases := map[string]func(t *testing.T, e *scoreEnv){
		"max_rounds": func(t *testing.T, e *scoreEnv) { mustExec(t, e.st, `UPDATE workflow_auto SET round = 1`) },
		"max_total_rounds": func(t *testing.T, e *scoreEnv) {
			e.setScope(t, func(s *model.AutoScope) { s.MaxRounds, s.MaxTotalRounds = 1, 1 })
			mustExec(t, e.st, `UPDATE workflow_auto SET total_rounds = 1`)
		},
		"max_tts_chars": func(t *testing.T, e *scoreEnv) { mustExec(t, e.st, `UPDATE workflow_auto SET tts_chars_used = 20000`) },
		"max_actions": func(t *testing.T, e *scoreEnv) {
			// Leave room for scoring (one action) but none for the rerun.
			e.setScope(t, func(s *model.AutoScope) { s.MaxActions = 2 })
			mustExec(t, e.st, `UPDATE workflow_auto SET actions_used = 0`)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newScoreEnv(t, 0.9, "团战")
			e.fullScope(t)
			e.setThresholds(t, low)
			pid := e.preview(t, true)
			setup(t, e)
			if name == "max_actions" {
				mustExec(t, e.st, `UPDATE workflow_auto SET actions_used = 0`)
			}
			review, v := e.scored(t, pid)
			if name == "max_actions" {
				mustExec(t, e.st, `UPDATE workflow_auto SET actions_used = 2`)
				v = e.auto(t).AutoVersion
			}
			got, err := e.route(t, review, v)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != model.AutoStateAwaitingDecision || got.HaltReason != model.AutoWaitExhausted || !got.DeliveryHold {
				t.Fatalf("overlay = %+v", got)
			}
			run, _ := e.st.GetWorkflow(t.Context(), e.run.RunID)
			if run.Status != model.WorkflowReadyAcceptance {
				t.Fatalf("an exhausted run was changed: %+v", run)
			}
			if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='rerun'`, e.run.RunID); n != 0 {
				t.Fatalf("rerun rows = %d", n)
			}
		})
	}
}

func TestRouteLowNotImprovableHalts(t *testing.T) {
	e := newScoreEnv(t, 0.9, "")
	e.fullScope(t)
	pid := e.preview(t, false)
	job, _ := e.st.GetPreviewJob(t.Context(), pid)
	mustExec(t, e.st, `UPDATE tasks SET status='failed' WHERE task_id=?`, job.TaskID)
	review, v := e.scored(t, pid)
	got, err := e.route(t, review, v)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.AutoStateHalted || got.HaltReason != model.AutoHaltNotImprovable {
		t.Fatalf("overlay = %+v", got)
	}
}

func TestRouteRuleBAssetBusyWaitsForTheUser(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	e.setThresholds(t, map[string]float64{autoreview.KeyHighScore: 0.99, autoreview.KeyMediumScore: 0.98})
	pid := e.preview(t, true)
	review, v := e.scored(t, pid)
	// An older run is still active on the asset (the newest run is the auto run,
	// so its preview stays current).
	mustExec(t, e.st, `INSERT INTO workflow_runs (run_id, asset_id, base_revision_id, current_revision_id, status, stage, version, content_mode, created_at, updated_at, error_code, blocked_reason)
		VALUES ('run-other', 'asset-score', 'rev-score-1', 'rev-score-1', 'running', 'recognize', 1, 'configured', ?, ?, '', '')`, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(-time.Hour))
	got, err := e.route(t, review, v)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.AutoStateAwaitingDecision || got.HaltReason != model.AutoWaitAssetBusy || !got.DeliveryHold {
		t.Fatalf("overlay = %+v", got)
	}
	run, _ := e.st.GetWorkflow(t.Context(), e.run.RunID)
	if run.Status != model.WorkflowReadyAcceptance || run.Version != e.run.Version {
		t.Fatalf("the busy asset's run was touched: %+v", run)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM workflow_auto_rounds WHERE run_id=?`, e.run.RunID); n != 0 {
		t.Fatalf("rounds = %d", n)
	}
	// A revise meets the same rule and is refused without any change.
	view, _ := e.svc.GetAuto(t.Context(), e.run.RunID)
	if _, _, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "revise", "再来一次", view.AutoVersion, "rev-busy"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("revise on a busy asset: %v", err)
	}
	if after := e.auto(t); after.State != model.AutoStateAwaitingDecision || after.AutoVersion != view.AutoVersion {
		t.Fatalf("a refused revise changed the overlay: %+v", after)
	}
}

func TestDecideReviseStartsARoundCountedInTotalOnly(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼")
	e.fullScope(t)
	clock := newFakeClock()
	e.svc.SetClock(clock.Now)
	pid := e.preview(t, true)
	review, v := e.scored(t, pid)
	if _, err := e.route(t, review, v); err != nil {
		t.Fatal(err)
	}
	waiting := e.auto(t)
	clock.Advance(3 * time.Hour) // past deadline_at, inside the decision window: the clock is paused
	if _, _, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "revise", "", waiting.AutoVersion, "r0"); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("revise without feedback: %v", err)
	}
	run, view, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "revise", "只要最后一波团战", waiting.AutoVersion, "r1")
	if err != nil {
		t.Fatal(err)
	}
	got := e.auto(t)
	if got.State != model.AutoStateActive || got.Round != 0 || got.TotalRounds != 1 || got.ActionsUsed != waiting.ActionsUsed ||
		got.PausedAt != nil || got.DecisionDeadlineAt != nil || got.HaltReason != "" ||
		!got.DeadlineAt.Equal(clock.Now().Add(7200*time.Second)) || got.AutoVersion != waiting.AutoVersion+1 {
		t.Fatalf("overlay = %+v", got)
	}
	if run.Status != model.WorkflowRunning || run.Stage != model.TaskTypeRecognize {
		t.Fatalf("run = %+v", run)
	}
	if len(view.Rounds) != 1 || !strings.Contains(view.Rounds[0].Feedback, "只要最后一波团战") {
		t.Fatalf("rounds = %+v", view.Rounds)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='rerun'`, e.run.RunID); n != 0 {
		t.Fatalf("a user's revise wrote %d ledger rows", n)
	}
	// Replaying the same key is a success and starts nothing more.
	if _, _, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "revise", "只要最后一波团战", waiting.AutoVersion, "r1"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM workflow_auto_rounds WHERE run_id=?`, e.run.RunID); n != 1 {
		t.Fatalf("rounds after replay = %d", n)
	}
}

func TestDecideReviseRefusedWhenACapIsSpent(t *testing.T) {
	for name, col := range map[string]string{"total_rounds": "total_rounds = 5", "actions": "actions_used = 100", "tts": "tts_chars_used = 20000"} {
		t.Run(name, func(t *testing.T) {
			e := newScoreEnv(t, 0.5, "钓鱼")
			e.fullScope(t)
			pid := e.preview(t, true)
			review, v := e.scored(t, pid)
			if _, err := e.route(t, review, v); err != nil {
				t.Fatal(err)
			}
			mustExec(t, e.st, `UPDATE workflow_auto SET `+col+` WHERE run_id = ?`, e.run.RunID)
			if _, _, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "revise", "再来", e.auto(t).AutoVersion, "k"); !errors.Is(err, model.ErrInvalidState) {
				t.Fatalf("revise past a cap: %v", err)
			}
			if e.auto(t).State != model.AutoStateAwaitingDecision {
				t.Fatal("a refused revise changed the state")
			}
		})
	}
}
