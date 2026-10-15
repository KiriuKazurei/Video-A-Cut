package service_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func tick(t *testing.T, env *autoEnv) {
	t.Helper()
	if err := env.svc.AutoTick(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (e *autoEnv) auto(t *testing.T, runID string) model.WorkflowAuto {
	t.Helper()
	a, err := storeOf(t, e.svc).GetWorkflowAuto(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (e *autoEnv) run(t *testing.T, runID string) model.WorkflowRun {
	t.Helper()
	r, _, err := e.svc.GetWorkflow(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func twoLines(first, second string, end1 float64) func(map[string]any) {
	return func(edl map[string]any) {
		edl["narration"] = []any{
			map[string]any{"id": "nar_scn_1", "text": first, "start": 0.2, "end": end1, "source_scene_id": "scn_1", "source_scene_label": "团战A"},
			map[string]any{"id": "nar_scn_2", "text": second, "start": 1.2, "end": 1.9, "source_scene_id": "scn_2", "source_scene_label": "团战B"},
		}
	}
}

// toDraftReview lets the driver confirm the scenes, then delivers the sort and
// narrate stages with the given narration.
func (e *autoEnv) toDraftReview(t *testing.T, narration func(map[string]any)) model.WorkflowRun {
	t.Helper()
	run, _ := e.atSceneReview(t, "", [2]float64{0.9, 0.8})
	tick(t, e)
	if got := e.run(t, run.RunID); got.Stage != model.TaskTypeSort {
		t.Fatalf("driver left the run at %s/%s", got.Stage, got.Status)
	}
	if err := e.deliverStage(t, "recognizer", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.deliverStage(t, "narrator", narration); err != nil {
		t.Fatal(err)
	}
	got := e.run(t, run.RunID)
	if got.Stage != "draft_review" {
		t.Fatalf("run at %s/%s", got.Stage, got.Status)
	}
	return got
}

func TestTickConfirmsScenesApprovesNarrationAndIsIdempotent(t *testing.T) {
	env := newAutoEnv(t, false)
	run := env.toDraftReview(t, goodNarration)
	tick(t, env)
	got := env.run(t, run.RunID)
	if got.Stage != model.TaskTypeTTS || got.Status != model.WorkflowRunning {
		t.Fatalf("after approval: %s/%s", got.Stage, got.Status)
	}
	a := env.auto(t, run.RunID)
	if a.State != model.AutoStateActive || a.ActionsUsed != 2 || a.TTSCharsUsed != 4 {
		t.Fatalf("overlay = %+v", a)
	}
	for kind, want := range map[string]int{"scene_confirm": 1, "narration_approve": 1} {
		if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind=?`, run.RunID, kind); n != want {
			t.Fatalf("%s ledger rows = %d", kind, n)
		}
	}
	// Another tick, as after a restart, changes nothing.
	tick(t, env)
	tick(t, env)
	if again := env.auto(t, run.RunID); again.AutoVersion != a.AutoVersion || again.ActionsUsed != a.ActionsUsed {
		t.Fatalf("a repeated tick changed the overlay: %+v -> %+v", a, again)
	}
	rows, _ := storeOf(t, env.svc).ListNarrationApprovalRows(t.Context(), run.RunID, run.CurrentRevisionID)
	if len(rows) != 1 || rows["nar_scn_1"].Actor != model.AutoActor {
		t.Fatalf("approvals = %+v", rows)
	}
	// The replay of the closure itself, with stale versions, is a success.
	if _, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, run.Version, 1, run.CurrentRevisionID); err != nil {
		t.Fatalf("replay: %v", err)
	}
}

func TestTickSceneCriteriaFailureBecomesARerunWithFeedback(t *testing.T) {
	env := newAutoEnv(t, false)
	run, _ := env.atSceneReview(t, "", [2]float64{0.3, 0.9}) // below scene_confidence_min
	tick(t, env)
	a := env.auto(t, run.RunID)
	got := env.run(t, run.RunID)
	if a.State != model.AutoStateActive || a.Round != 1 || a.TotalRounds != 1 || got.Stage != model.TaskTypeRecognize || got.Status != model.WorkflowRunning {
		t.Fatalf("overlay = %+v run = %s/%s", a, got.Stage, got.Status)
	}
	if got.CurrentRevisionID != got.BaseRevisionID {
		t.Fatalf("rerun must restart from the base revision: %+v", got)
	}
	tk, err := env.svc.ClaimTask(t.Context(), "recognizer", "recognizer", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	in, err := env.svc.GetTaskInput(t.Context(), "recognizer", "recognizer", tk.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if in.Auto == nil || in.Auto.Round != 1 || len(in.Auto.Feedback) != 1 || in.Auto.Feedback[0].Code == "" || len(in.Auto.FeedbackText) != 1 || in.Auto.FeedbackText[0] == "" {
		t.Fatalf("task input auto block = %+v", in.Auto)
	}
}

func TestTickNarrationCriteria(t *testing.T) {
	cases := map[string]struct {
		edit func(map[string]any)
		code string
	}{
		"pace":    {twoLines("这一句话写得实在太长太长太长太长太长太长太长了", "好", 0.5), "pacing_out_of_range"},
		"url":     {twoLines("见x.cn", "好", 1.0), "item_below_floor"},
		"contact": {twoLines("加微信吧", "好", 1.0), "item_below_floor"},
		"digits":  {twoLines("13800138", "好", 1.0), "item_below_floor"},
		"overlap": {func(edl map[string]any) {
			edl["narration"] = []any{
				map[string]any{"id": "nar_scn_1", "text": "第一", "start": 0.2, "end": 0.7, "source_scene_id": "scn_1", "source_scene_label": "团战A"},
				map[string]any{"id": "nar_scn_x", "text": "第二", "start": 0.5, "end": 0.9, "source_scene_id": "scn_1", "source_scene_label": "团战A"},
			}
		}, "item_below_floor"},
		"label": {func(edl map[string]any) {
			edl["narration"] = []any{map[string]any{"id": "nar_scn_1", "text": "团战开始", "start": 0.2, "end": 1.0, "source_scene_id": "scn_1", "source_scene_label": "别的标签"}}
		}, "item_below_floor"},
		"empty": {func(edl map[string]any) { edl["narration"] = []any{} }, "tts_incomplete"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := newAutoEnv(t, false)
			run := env.toDraftReview(t, c.edit)
			before := env.auto(t, run.RunID)
			// Ask for the criteria directly: nothing may be approved.
			_, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, run.Version, before.AutoVersion, run.CurrentRevisionID)
			var pe interface{ Error() string }
			if !errors.As(err, &pe) || !strings.Contains(err.Error(), c.code) || !errors.Is(err, model.ErrInvalidState) {
				t.Fatalf("err = %v, want code %s", err, c.code)
			}
			if rows, _ := storeOf(t, env.svc).ListNarrationApprovalRows(t.Context(), run.RunID, run.CurrentRevisionID); len(rows) != 0 {
				t.Fatalf("a refused batch approved %d lines", len(rows))
			}
			if env.auto(t, run.RunID).AutoVersion != before.AutoVersion {
				t.Fatal("a refusal changed the overlay")
			}
			// The driver turns it into a rerun with that code as feedback.
			tick(t, env)
			a := env.auto(t, run.RunID)
			if a.Round != 1 || env.run(t, run.RunID).Stage != model.TaskTypeRecognize {
				t.Fatalf("no rerun: %+v", a)
			}
			view, _ := env.svc.GetAuto(t.Context(), run.RunID)
			if len(view.Rounds) != 1 || !strings.Contains(view.Rounds[0].Feedback, `"code":"`+c.code+`"`) {
				t.Fatalf("feedback = %+v", view.Rounds)
			}
		})
	}
}

func TestTickSkipsHumanApprovedLineAndHaltsOnDisagreement(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		env := newAutoEnv(t, false)
		run := env.toDraftReview(t, twoLines("团战开始", "再来一波", 0.9))
		if _, err := env.svc.ApproveWorkflowNarration(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "nar_scn_1"); err != nil {
			t.Fatal(err)
		}
		tick(t, env)
		got := env.run(t, run.RunID)
		if got.Stage != model.TaskTypeTTS {
			t.Fatalf("run at %s", got.Stage)
		}
		rows, _ := storeOf(t, env.svc).ListNarrationApprovalRows(t.Context(), run.RunID, run.CurrentRevisionID)
		if rows["nar_scn_1"].Actor != "human:webui" || rows["nar_scn_2"].Actor != model.AutoActor {
			t.Fatalf("approvals = %+v", rows)
		}
		if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='skip_human_decided' AND target='nar_scn_1'`, run.RunID); n != 1 {
			t.Fatalf("skip decisions = %d", n)
		}
		if a := env.auto(t, run.RunID); a.State != model.AutoStateActive {
			t.Fatalf("a human approval took the run over: %+v", a)
		}
	})
	t.Run("clash", func(t *testing.T) {
		env := newAutoEnv(t, false)
		run := env.toDraftReview(t, goodNarration)
		if err := storeOf(t, env.svc).InsertNarrationApproval(t.Context(), run.RunID, run.CurrentRevisionID, "nar_scn_1",
			strings.Repeat("a", 64), "human:webui", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		before := env.auto(t, run.RunID)
		if _, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, run.Version, before.AutoVersion, run.CurrentRevisionID); !errors.Is(err, model.ErrConflict) {
			t.Fatalf("clash: %v", err)
		}
		a := env.auto(t, run.RunID)
		if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltHumanDecisionClash {
			t.Fatalf("overlay = %+v", a)
		}
		if rows, _ := storeOf(t, env.svc).ListNarrationApprovalRows(t.Context(), run.RunID, run.CurrentRevisionID); rows["nar_scn_1"].Actor != "human:webui" {
			t.Fatalf("the person's row was overwritten: %+v", rows)
		}
		if got := env.run(t, run.RunID); got.Stage != "draft_review" {
			t.Fatalf("run moved: %s", got.Stage)
		}
	})
}

func TestTickHaltsWhenTTSBudgetWouldBeExceeded(t *testing.T) {
	env := newAutoEnv(t, false)
	run := env.toDraftReview(t, goodNarration)
	mustExec(t, storeOf(t, env.svc), `UPDATE workflow_auto SET scope_json = json_set(scope_json, '$.max_tts_chars', 3) WHERE run_id = ?`, run.RunID)
	tick(t, env)
	a := env.auto(t, run.RunID)
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltMaxTTSChars {
		t.Fatalf("overlay = %+v", a)
	}
	if rows, _ := storeOf(t, env.svc).ListNarrationApprovalRows(t.Context(), run.RunID, run.CurrentRevisionID); len(rows) != 0 {
		t.Fatal("lines were approved past the cap")
	}
}

func TestTickRetriesAFailedStageOnceThenHalts(t *testing.T) {
	env := newAutoEnv(t, false)
	run, _ := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	tick(t, env) // sort queued
	fail := func() {
		tk, err := env.svc.ClaimTask(t.Context(), "recognizer", "recognizer", time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := env.svc.FailTask(t.Context(), "recognizer", tk.TaskID, "boom"); err != nil {
			t.Fatal(err)
		}
	}
	fail()
	if got := env.run(t, run.RunID); got.Status != model.WorkflowFailed {
		t.Fatalf("run = %+v", got)
	}
	tick(t, env)
	got := env.run(t, run.RunID)
	if got.Status != model.WorkflowRunning || got.Stage != model.TaskTypeSort {
		t.Fatalf("no retry: %s/%s", got.Stage, got.Status)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='retry' AND target='sort'`, run.RunID); n != 1 {
		t.Fatalf("retry rows = %d", n)
	}
	fail()
	tick(t, env)
	a := env.auto(t, run.RunID)
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltStageFailed {
		t.Fatalf("overlay = %+v", a)
	}
	if got := env.run(t, run.RunID); got.Status != model.WorkflowFailed {
		t.Fatalf("a halted run must be left for the person: %+v", got)
	}
}

func TestTickDeadlineAppliesOnlyWhileActive(t *testing.T) {
	t.Run("active past deadline halts", func(t *testing.T) {
		env := newAutoEnv(t, false)
		clock := newFakeClock()
		env.svc.SetClock(clock.Now)
		run, _ := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
		clock.Advance(2*time.Hour + time.Minute) // default max_wall_seconds 7200
		tick(t, env)
		a := env.auto(t, run.RunID)
		if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltDeadline {
			t.Fatalf("overlay = %+v", a)
		}
		if got := env.run(t, run.RunID); got.Stage != "scene_review" {
			t.Fatal("a halted run kept moving")
		}
	})
	t.Run("waiting pauses the clock until the decision timeout", func(t *testing.T) {
		env := newAutoEnv(t, false)
		clock := newFakeClock()
		env.svc.SetClock(clock.Now)
		run, _ := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
		st := storeOf(t, env.svc)
		paused, deadline := clock.Now(), clock.Now().Add(72*time.Hour)
		mustExec(t, st, `UPDATE workflow_auto SET state='awaiting_user_decision', halt_reason='medium', delivery_hold=1, paused_at=?, decision_deadline_at=? WHERE run_id=?`, paused, deadline, run.RunID)
		clock.Advance(71 * time.Hour) // far past deadline_at (2h), inside the decision window
		tick(t, env)
		if a := env.auto(t, run.RunID); a.State != model.AutoStateAwaitingDecision {
			t.Fatalf("the wall clock ran while waiting: %+v", a)
		}
		clock.Advance(2 * time.Hour)
		tick(t, env)
		a := env.auto(t, run.RunID)
		if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltDecisionTimeout || !a.DeliveryHold {
			t.Fatalf("overlay = %+v", a)
		}
		tick(t, env) // halted: nothing more
	})
}

func TestTickRevokedAndCancelledRunsAreLeftAlone(t *testing.T) {
	env := newAutoEnv(t, false)
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	if _, err := env.svc.RevokeAuto(t.Context(), "human:webui", run.RunID, "stop", view.AutoVersion, false); err != nil {
		t.Fatal(err)
	}
	tick(t, env)
	if got := env.run(t, run.RunID); got.Stage != "scene_review" || got.Status != model.WorkflowAwaitingReview {
		t.Fatalf("the driver acted on a revoked run: %+v", got)
	}
	// Closures called directly by a stale driver are refused too.
	a := env.auto(t, run.RunID)
	if _, err := env.svc.AutoConfirmScene(t.Context(), run.RunID, run.Version, a.AutoVersion, run.CurrentRevisionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("confirm after revoke: %v", err)
	}
	if _, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, run.Version, a.AutoVersion, run.CurrentRevisionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("approve after revoke: %v", err)
	}
	if err := env.svc.AutoRerunForPolicy(t.Context(), run.RunID, a.AutoVersion, []string{"scene_criteria_failed"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("rerun after revoke: %v", err)
	}
	if _, err := env.svc.RetryWorkflow(t.Context(), model.AutoActor, run.RunID, "recognize", "k", run.Version); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("auto actor through the human retry: %v", err)
	}
}

func TestAutoClosuresRefuseARunThatIsNotAuto(t *testing.T) {
	svc, root, run := workflowSetup(t)
	gate := workflowDraftGate(t, svc, root)
	if _, err := svc.AutoApproveNarration(t.Context(), run.RunID, gate.Version, 1, gate.CurrentRevisionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("approve on a manual run: %v", err)
	}
	if err := svc.AutoTick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := svc.GetWorkflow(t.Context(), run.RunID); got.Version != gate.Version {
		t.Fatal("the driver touched a manual run")
	}
}

func TestTickAcceptancePathStartsPreviewScoresAndRoutes(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	e.prepPreview(t)
	tickOnce := func() {
		if err := e.svc.AutoTick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	tickOnce() // starts the preview
	jobs, _ := e.st.ListPreviewJobs(t.Context(), e.run.RunID)
	if len(jobs) != 1 || jobs[0].CreatedBy != model.AutoActor || !strings.HasPrefix(jobs[0].RequestID, "auto:"+e.run.RunID+":0:preview") {
		t.Fatalf("jobs = %+v", jobs)
	}
	if a := e.auto(t); a.ActionsUsed != 1 || a.State != model.AutoStateActive {
		t.Fatalf("overlay = %+v", a)
	}
	tickOnce() // preview still queued: wait
	if a := e.auto(t); a.ActionsUsed != 1 {
		t.Fatalf("waiting spent an action: %+v", a)
	}
	e.finishPreview(t, jobs[0].PreviewID)
	tickOnce() // scores and routes HIGH
	a := e.auto(t)
	if a.State != model.AutoStateCompleted || a.DeliveryHold || a.ActionsUsed != 2 {
		t.Fatalf("overlay = %+v", a)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_reviews WHERE run_id=?`, e.run.RunID); n != 1 {
		t.Fatalf("reviews = %d", n)
	}
	tickOnce()
	if again := e.auto(t); again.AutoVersion != a.AutoVersion {
		t.Fatal("a tick after completion changed the overlay")
	}
}

func TestTickRestartAfterScoringRoutesTheStoredReviewOnce(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	e.setThresholds(t, map[string]float64{"high_score_min": 0.99, "medium_score_min": 0.98})
	pid := e.preview(t, true)
	// The process died after scoring, before routing.
	e.scored(t, pid)
	for i := 0; i < 3; i++ {
		if err := e.svc.AutoTick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	a := e.auto(t)
	if a.Round != 1 || a.TotalRounds != 1 {
		t.Fatalf("overlay = %+v", a)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_reviews WHERE run_id=?`, e.run.RunID); n != 1 {
		t.Fatalf("reviews = %d", n)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='rerun'`, e.run.RunID); n != 1 {
		t.Fatalf("reruns = %d", n)
	}
}

func TestRevokedRunRefusesPreviewAndRouting(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	pid := e.preview(t, true)
	review, v := e.scored(t, pid)
	if _, err := e.svc.RevokeAuto(t.Context(), "human:webui", e.run.RunID, "stop", v, false); err != nil {
		t.Fatal(err)
	}
	v = e.auto(t).AutoVersion
	if _, err := e.route(t, review, v); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("route after revoke: %v", err)
	}
	// A new request (another round) must be refused: the job is the only thing
	// the same request could return.
	mustExec(t, e.st, `UPDATE workflow_auto SET total_rounds = 1 WHERE run_id = ?`, e.run.RunID)
	if _, _, err := e.svc.AutoStartPreview(t.Context(), e.run.RunID, e.run.Version, e.auto(t).AutoVersion, e.rev.RevisionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("start after revoke: %v", err)
	}
	mustExec(t, e.st, `UPDATE workflow_auto SET total_rounds = 0 WHERE run_id = ?`, e.run.RunID)
	if err := e.svc.AutoTick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if a := e.auto(t); a.State != model.AutoStateRevoked || a.AutoVersion != v {
		t.Fatalf("overlay = %+v", a)
	}
}

func TestTickPolicyFailureBeyondMaxRoundsWaitsForTheUser(t *testing.T) {
	env := newAutoEnv(t, false)
	run, _ := env.atSceneReview(t, "", [2]float64{0.3, 0.9})
	mustExec(t, storeOf(t, env.svc), `UPDATE workflow_auto SET round = 1 WHERE run_id = ?`, run.RunID)
	tick(t, env)
	a := env.auto(t, run.RunID)
	if a.State != model.AutoStateAwaitingDecision || a.HaltReason != model.AutoWaitExhausted || !a.DeliveryHold || a.PausedAt == nil {
		t.Fatalf("overlay = %+v", a)
	}
	if got := env.run(t, run.RunID); got.Stage != "scene_review" {
		t.Fatalf("an exhausted run moved: %+v", got)
	}
}
