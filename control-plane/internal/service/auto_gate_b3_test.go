package service_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// Tests added for Gate's B3 review (finding 3): the places a mutation survived.

func (e *scoreEnv) humanPreview(t *testing.T, requestID string) (preview.View, bool, error) {
	t.Helper()
	return e.svc.StartPreview(t.Context(), "human:test", e.run.RunID, preview.StartRequest{
		RevisionID: e.rev.RevisionID, ExpectedVersion: e.run.Version, PresetID: preview.PresetID, RequestID: requestID})
}

func (e *scoreEnv) assertNotTakenOver(t *testing.T, label string, wantVersion int) {
	t.Helper()
	a := e.auto(t)
	if a.State != model.AutoStateActive || a.AutoVersion != wantVersion {
		t.Fatalf("%s: a reused preview took the run over: %+v", label, a)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='human_takeover'`, e.run.RunID); n != 0 {
		t.Fatalf("%s: takeover decisions = %d", label, n)
	}
	if got := len(auditActions(t, e.svc)["auto.takeover"]); got != 0 {
		t.Fatalf("%s: takeover audit rows = %d", label, got)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM preview_jobs WHERE run_id=?`, e.run.RunID); n != 1 {
		t.Fatalf("%s: preview jobs = %d, want the one the auto-reviewer made", label, n)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM tasks WHERE asset_id=? AND type='preview'`, e.run.AssetID); n != 1 {
		t.Fatalf("%s: preview tasks = %d", label, n)
	}
}

// A person asking for a preview the auto-reviewer already has does not stop the
// run. There are three ways a start can reuse a job: the request id already
// names one (checked before the transaction), the request id names one that
// appeared in the gap (checked in the transaction), and another request already
// made an identical one (found in the transaction). None is a takeover.
func TestHumanPreviewReuseNeverTakesTheRunOver(t *testing.T) {
	autoReq := "auto:run-score:0:preview"
	t.Run("same request id, before the transaction", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "团战")
		e.prepPreview(t)
		if _, _, err := e.svc.AutoStartPreview(t.Context(), e.run.RunID, e.run.Version, 1, e.rev.RevisionID); err != nil {
			t.Fatal(err)
		}
		_, created, err := e.humanPreview(t, autoReq)
		if err != nil || created {
			t.Fatalf("created=%v err=%v", created, err)
		}
		e.assertNotTakenOver(t, "request id", 2)
	})
	t.Run("same request id appears while the transaction is being prepared", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "团战")
		e.prepPreview(t)
		e.svc.SetBeforePreviewTx(func() {
			e.svc.SetBeforePreviewTx(nil)
			if _, _, err := e.svc.AutoStartPreview(t.Context(), e.run.RunID, e.run.Version, 1, e.rev.RevisionID); err != nil {
				t.Errorf("auto start in the gap: %v", err)
			}
		})
		_, created, err := e.humanPreview(t, autoReq)
		if err != nil || created {
			t.Fatalf("created=%v err=%v", created, err)
		}
		e.assertNotTakenOver(t, "in-transaction request id", 2)
	})
	t.Run("another request id, identical input", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "团战")
		e.prepPreview(t)
		if _, _, err := e.svc.AutoStartPreview(t.Context(), e.run.RunID, e.run.Version, 1, e.rev.RevisionID); err != nil {
			t.Fatal(err)
		}
		_, created, err := e.humanPreview(t, "req-human-other")
		if err != nil || created {
			t.Fatalf("created=%v err=%v", created, err)
		}
		e.assertNotTakenOver(t, "reusable job", 2)
		// The reuse bound the person's request id to the same job.
		if _, created, err := e.humanPreview(t, "req-human-other"); err != nil || created {
			t.Fatalf("replay: created=%v err=%v", created, err)
		}
	})
}

// Only a live overlay is changed by a person's action. One that was revoked,
// completed, halted or cancelled is history: rewriting it would turn "the user
// revoked" or "finished" into "halted" and add decisions to a closed run.
func TestHumanActionsDoNotRewriteAnOverlayThatIsNotLive(t *testing.T) {
	for _, st := range []struct{ state, reason string }{
		{model.AutoStateRevoked, ""}, {model.AutoStateCompleted, ""},
		{model.AutoStateHalted, model.AutoHaltDeadline}, {model.AutoStateHalted, model.AutoHaltHumanTakeover},
		{model.AutoStateCancelled, ""},
	} {
		t.Run(st.state+"/"+st.reason, func(t *testing.T) {
			e := newScoreEnv(t, 0.9, "团战")
			e.prepPreview(t)
			mustExec(t, e.st, `UPDATE workflow_auto SET state = ?, halt_reason = ? WHERE run_id = ?`, st.state, st.reason, e.run.RunID)
			before := e.auto(t)
			same := func(step string) {
				t.Helper()
				got := e.auto(t)
				if got.State != before.State || got.HaltReason != before.HaltReason || got.AutoVersion != before.AutoVersion ||
					got.DeliveryHold != before.DeliveryHold {
					t.Fatalf("%s rewrote the overlay: %+v -> %+v", step, before, got)
				}
				if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=?`, e.run.RunID); n != 0 {
					t.Fatalf("%s wrote %d decisions on a closed overlay", step, n)
				}
				if got := len(auditActions(t, e.svc)["auto.takeover"]) + len(auditActions(t, e.svc)["auto.human_completed"]) +
					len(auditActions(t, e.svc)["auto.cancel"]); got != 0 {
					t.Fatalf("%s wrote %d auto audit rows", step, got)
				}
			}
			if _, created, err := e.humanPreview(t, "req-human-new"); err != nil || !created {
				t.Fatalf("human preview: created=%v err=%v", created, err)
			}
			same("a new human preview")
			if _, err := e.svc.RecordAcceptance(t.Context(), "human:webui", e.run.RunID, "browser", "passed", "", e.runVersion(t)); err != nil {
				t.Fatal(err)
			}
			same("acceptance")
			if _, err := e.svc.CancelWorkflow(t.Context(), "human:webui", e.run.RunID, "stop", e.runVersion(t)); err != nil {
				t.Fatal(err)
			}
			same("cancel")
		})
	}
}

// autoGuard is the check every automatic action makes against the overlay it has
// just read. Each refusal has its own cause and the order of the checks is part
// of the contract: a person reading "past its deadline" must not be told "version
// conflict" for the same overlay.
func TestAutoGuardRefusalsAndOrder(t *testing.T) {
	now := time.Now().UTC()
	live := model.WorkflowAuto{State: model.AutoStateActive, AutoVersion: 4, DeadlineAt: now.Add(time.Minute), ActionsUsed: 1,
		Scope: model.AutoScope{MaxActions: 2}}
	if err := service.AutoGuard(live, true, 4, now); err != nil {
		t.Fatalf("a live, in-scope, in-budget overlay: %v", err)
	}
	at := live
	at.DeadlineAt = now
	if err := service.AutoGuard(at, true, 4, now); err != nil {
		t.Fatalf("at the deadline instant the run is still inside it: %v", err)
	}
	for _, state := range []string{model.AutoStateAwaitingDecision, model.AutoStateHalted, model.AutoStateRevoked, model.AutoStateCompleted, model.AutoStateCancelled} {
		a := live
		a.State = state
		if err := service.AutoGuard(a, true, 4, now); !errors.Is(err, model.ErrForbidden) || !strings.Contains(err.Error(), state) {
			t.Fatalf("state %s: %v", state, err)
		}
	}
	if err := service.AutoGuard(live, false, 4, now); !errors.Is(err, model.ErrForbidden) || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("out of scope: %v", err)
	}
	late := live
	late.DeadlineAt = now.Add(-time.Nanosecond)
	if err := service.AutoGuard(late, true, 4, now); !errors.Is(err, model.ErrForbidden) || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("past the deadline: %v", err)
	}
	spent := live
	spent.ActionsUsed = 2
	if err := service.AutoGuard(spent, true, 4, now); !errors.Is(err, service.ErrAutoBudgetSpent) {
		t.Fatalf("budget: %v", err)
	}
	if err := service.AutoGuard(live, true, 3, now); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("version: %v", err)
	}
	// Order: state, scope, deadline, budget, version.
	all := late
	all.ActionsUsed = 2
	if err := service.AutoGuard(all, false, 3, now); !strings.Contains(err.Error(), "scope") {
		t.Fatalf("scope must come before deadline/budget/version: %v", err)
	}
	if err := service.AutoGuard(all, true, 3, now); !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("deadline must come before budget/version: %v", err)
	}
	all.DeadlineAt = now.Add(time.Minute)
	if err := service.AutoGuard(all, true, 3, now); !errors.Is(err, service.ErrAutoBudgetSpent) {
		t.Fatalf("budget must come before version: %v", err)
	}
	inactive := all
	inactive.State = model.AutoStateHalted
	if err := service.AutoGuard(inactive, false, 3, now); !strings.Contains(err.Error(), "halted") {
		t.Fatalf("state must come first: %v", err)
	}
}

// The same guard, reached through a real action: a refusal writes nothing.
func TestAutoStartPreviewGuardBranchesWriteNothing(t *testing.T) {
	cases := map[string]struct {
		prep func(e *scoreEnv, clock *fakeClock)
		want func(error) bool
	}{
		"deadline": {func(e *scoreEnv, c *fakeClock) { c.Advance(2 * time.Hour) },
			func(err error) bool {
				return errors.Is(err, model.ErrForbidden) && strings.Contains(err.Error(), "deadline")
			}},
		"budget": {func(e *scoreEnv, c *fakeClock) {
			mustExec(t, e.st, `UPDATE workflow_auto SET actions_used = 10 WHERE run_id = ?`, e.run.RunID)
		}, func(err error) bool { return errors.Is(err, service.ErrAutoBudgetSpent) }},
		"scope": {func(e *scoreEnv, c *fakeClock) {
			e.setScope(t, func(s *model.AutoScope) { s.AutoPreviewExport = false })
		}, func(err error) bool {
			return errors.Is(err, model.ErrForbidden) && strings.Contains(err.Error(), "scope")
		}},
		"state": {func(e *scoreEnv, c *fakeClock) {
			mustExec(t, e.st, `UPDATE workflow_auto SET state = 'halted' WHERE run_id = ?`, e.run.RunID)
		}, func(err error) bool { return errors.Is(err, model.ErrForbidden) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newScoreEnv(t, 0.9, "团战")
			e.prepPreview(t)
			clock := newFakeClock()
			e.svc.SetClock(clock.Now)
			// deadline_at was set against the wall clock when the env was made.
			mustExec(t, e.st, `UPDATE workflow_auto SET deadline_at = ? WHERE run_id = ?`, clock.Now().Add(time.Hour), e.run.RunID)
			c.prep(e, clock)
			before := e.auto(t)
			_, _, err := e.svc.AutoStartPreview(t.Context(), e.run.RunID, e.run.Version, before.AutoVersion, e.rev.RevisionID)
			if err == nil || !c.want(err) {
				t.Fatalf("err = %v", err)
			}
			if got := e.auto(t); got.AutoVersion != before.AutoVersion || got.ActionsUsed != before.ActionsUsed {
				t.Fatalf("a refusal changed the overlay: %+v", got)
			}
			for _, q := range []string{`SELECT COUNT(*) FROM preview_jobs`, `SELECT COUNT(*) FROM auto_actions`, `SELECT COUNT(*) FROM tasks WHERE type='preview'`} {
				if n := count(t, e.svc, q); n != 0 {
					t.Fatalf("%s = %d after a refusal", q, n)
				}
			}
		})
	}
}

// If a stored part of a review cannot be encoded, nothing is written: no review
// row, no ledger row, no budget or version spent, no event, and no "{}" stand-in.
func TestScoreAutoRunEncodingFailureWritesNothing(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} { // reasons, inputs, thresholds
		e := newScoreEnv(t, 0.9, "团战")
		pid := e.preview(t, true)
		before := e.auto(t)
		calls := 0
		e.svc.SetReviewEncoder(func(v any) ([]byte, error) {
			calls++
			if calls == failAt {
				return nil, errors.New("cannot encode")
			}
			return json.Marshal(v)
		})
		_, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, before.AutoVersion)
		if err == nil || !strings.Contains(err.Error(), "encode review") {
			t.Fatalf("failAt %d: err = %v", failAt, err)
		}
		if got := e.auto(t); got.AutoVersion != before.AutoVersion || got.ActionsUsed != before.ActionsUsed {
			t.Fatalf("failAt %d: overlay changed: %+v", failAt, got)
		}
		for _, q := range []string{`SELECT COUNT(*) FROM auto_reviews`, `SELECT COUNT(*) FROM auto_actions WHERE kind='score'`,
			`SELECT COUNT(*) FROM auto_decisions`} {
			if n := count(t, e.svc, q); n != 0 {
				t.Fatalf("failAt %d: %s = %d", failAt, q, n)
			}
		}
		if got := e.events.named("auto.reviewed", 1); len(got) != 0 {
			t.Fatalf("failAt %d: an event for a review that was not stored", failAt)
		}
		// With a working encoder the same call succeeds and stores real facts.
		e.svc.SetReviewEncoder(nil)
		review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, before.AutoVersion)
		if err != nil || string(review.Reasons) == "{}" || string(review.Inputs) == "{}" || string(review.Thresholds) == "{}" {
			t.Fatalf("failAt %d: retry: %v %+v", failAt, err, review)
		}
	}
}

// A replay of a committed approval is a success that announces nothing. Both
// ways to reach it are covered: the early check, and the in-transaction check
// when the first call commits between the early check and the transaction.
func TestAutoConfirmSceneReplayAnnouncesNothing(t *testing.T) {
	for _, inTx := range []bool{false, true} {
		name := "early replay"
		if inTx {
			name = "replay inside the transaction"
		}
		t.Run(name, func(t *testing.T) {
			env := newAutoEnv(t, false)
			rec := watchAuto(env)
			run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
			confirm := func() error {
				_, err := env.svc.AutoConfirmScene(t.Context(), run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
				return err
			}
			if inTx {
				env.svc.SetBeforeAutoTx(func() {
					env.svc.SetBeforeAutoTx(nil)
					if err := confirm(); err != nil {
						t.Errorf("first call: %v", err)
					}
				})
			}
			if err := confirm(); err != nil {
				t.Fatal(err)
			}
			if !inTx {
				if err := confirm(); err != nil {
					t.Fatalf("replay: %v", err)
				}
			}
			if got := rec.named("auto.changed", 2); len(got) != 1 {
				t.Fatalf("auto.changed events = %d, want exactly 1", len(got))
			}
			if got := rec.named("review.changed", 2); len(got) > 1 {
				t.Fatalf("review.changed events = %d", len(got))
			}
			if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='scene_confirm'`, run.RunID); n != 1 {
				t.Fatalf("ledger rows = %d", n)
			}
			if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='scene_confirm'`, run.RunID); n != 2 {
				t.Fatalf("scene_confirm decisions = %d, want one per scene", n)
			}
		})
	}
}

func TestAutoApproveNarrationReplayAnnouncesNothing(t *testing.T) {
	for _, inTx := range []bool{false, true} {
		name := "early replay"
		if inTx {
			name = "replay inside the transaction"
		}
		t.Run(name, func(t *testing.T) {
			env := newAutoEnv(t, false)
			run := env.toDraftReview(t, goodNarration)
			rec := watchAuto(env)
			a := env.auto(t, run.RunID)
			approve := func() error {
				_, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, run.Version, a.AutoVersion, run.CurrentRevisionID)
				return err
			}
			if inTx {
				env.svc.SetBeforeAutoTx(func() {
					env.svc.SetBeforeAutoTx(nil)
					if err := approve(); err != nil {
						t.Errorf("first call: %v", err)
					}
				})
			}
			if err := approve(); err != nil {
				t.Fatal(err)
			}
			if !inTx {
				if err := approve(); err != nil {
					t.Fatalf("replay: %v", err)
				}
			}
			if got := rec.named("auto.changed", 2); len(got) != 1 {
				t.Fatalf("auto.changed events = %d, want exactly 1", len(got))
			}
			if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='narration_approve'`, run.RunID); n != 1 {
				t.Fatalf("ledger rows = %d", n)
			}
			if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='narration_approve'`, run.RunID); n != 1 {
				t.Fatalf("narration_approve decisions = %d", n)
			}
		})
	}
}

// The approvals only apply at their own gate: AutoApproveNarration needs
// draft_review/awaiting_review, AutoConfirmScene needs scene_review/awaiting_review.
func TestAutoApprovalsRefuseTheWrongStageAndStatus(t *testing.T) {
	t.Run("narration at scene review", func(t *testing.T) {
		env := newAutoEnv(t, false)
		run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
		if _, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID); !errors.Is(err, model.ErrInvalidState) {
			t.Fatalf("err = %v", err)
		}
		if n := count(t, env.svc, `SELECT COUNT(*) FROM workflow_narration_approvals`); n != 0 {
			t.Fatalf("approvals = %d", n)
		}
		if env.auto(t, run.RunID).AutoVersion != view.AutoVersion {
			t.Fatal("a refusal changed the overlay")
		}
	})
	t.Run("narration while the run is not awaiting review", func(t *testing.T) {
		env := newAutoEnv(t, false)
		run := env.toDraftReview(t, goodNarration)
		a := env.auto(t, run.RunID)
		mustExec(t, storeOf(t, env.svc), `UPDATE workflow_runs SET status = 'running' WHERE run_id = ?`, run.RunID)
		if _, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, run.Version, a.AutoVersion, run.CurrentRevisionID); !errors.Is(err, model.ErrInvalidState) {
			t.Fatalf("err = %v", err)
		}
		if n := count(t, env.svc, `SELECT COUNT(*) FROM workflow_narration_approvals`); n != 0 {
			t.Fatalf("approvals = %d", n)
		}
	})
	t.Run("scene confirm at draft review", func(t *testing.T) {
		env := newAutoEnv(t, false)
		run := env.toDraftReview(t, goodNarration)
		a := env.auto(t, run.RunID)
		if _, err := env.svc.AutoConfirmScene(t.Context(), run.RunID, run.Version, a.AutoVersion, run.CurrentRevisionID); !errors.Is(err, model.ErrInvalidState) {
			t.Fatalf("err = %v", err)
		}
	})
}

type decisionRow struct{ Kind, Target, Actor string }

func decisions(t *testing.T, env *autoEnv, runID, kind string) []decisionRow {
	t.Helper()
	rows, err := storeOf(t, env.svc).DB().QueryContext(t.Context(), `SELECT kind, target, actor FROM auto_decisions WHERE run_id=? AND kind=? ORDER BY id`, runID, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []decisionRow
	for rows.Next() {
		var d decisionRow
		if err := rows.Scan(&d.Kind, &d.Target, &d.Actor); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

// Every automatic approval and every line it left to a person is in the
// retention table, with the actor that decided: the auto-reviewer for the rows
// it wrote, the person for the rows it skipped.
func TestApprovalsRecordApproveAndSkipHumanDecisionRows(t *testing.T) {
	t.Run("scenes", func(t *testing.T) {
		env := newAutoEnv(t, false)
		run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
		if _, err := env.svc.ConfirmScene(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "confirmed", ""); err != nil {
			t.Fatal(err)
		}
		cur := env.run(t, run.RunID)
		if _, err := env.svc.AutoConfirmScene(t.Context(), run.RunID, cur.Version, view.AutoVersion, cur.CurrentRevisionID); err != nil {
			t.Fatal(err)
		}
		if got := decisions(t, env, run.RunID, model.AutoDecisionSceneConfirm); len(got) != 1 || got[0].Target != "scn_2" || got[0].Actor != model.AutoActor {
			t.Fatalf("scene_confirm = %+v", got)
		}
		if got := decisions(t, env, run.RunID, model.AutoDecisionSkipHuman); len(got) != 1 || got[0].Target != "scn_1" || got[0].Actor != "human:webui" {
			t.Fatalf("skip_human_decided = %+v", got)
		}
		if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='scene_confirm'`, run.RunID); n != 1 {
			t.Fatalf("ledger rows = %d: a skipped scene must not cost an action", n)
		}
	})
	t.Run("narration", func(t *testing.T) {
		env := newAutoEnv(t, false)
		run := env.toDraftReview(t, twoLines("团战开始", "再来一波", 0.9))
		if _, err := env.svc.ApproveWorkflowNarration(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "nar_scn_1"); err != nil {
			t.Fatal(err)
		}
		cur := env.run(t, run.RunID)
		if _, err := env.svc.AutoApproveNarration(t.Context(), run.RunID, cur.Version, env.auto(t, run.RunID).AutoVersion, cur.CurrentRevisionID); err != nil {
			t.Fatal(err)
		}
		if got := decisions(t, env, run.RunID, model.AutoDecisionNarration); len(got) != 1 || got[0].Target != "nar_scn_2" || got[0].Actor != model.AutoActor {
			t.Fatalf("narration_approve = %+v", got)
		}
		if got := decisions(t, env, run.RunID, model.AutoDecisionSkipHuman); len(got) != 1 || got[0].Target != "nar_scn_1" || got[0].Actor != "human:webui" {
			t.Fatalf("skip_human_decided = %+v", got)
		}
	})
}
