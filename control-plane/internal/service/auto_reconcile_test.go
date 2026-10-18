package service_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// toWaiting routes a MEDIUM review so the overlay waits for the user with the
// delivery hold set.
func (e *scoreEnv) toWaiting(t *testing.T) {
	t.Helper()
	e.fullScope(t)
	pid := e.preview(t, true)
	review, v := e.scored(t, pid)
	got, err := e.route(t, review, v)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.AutoStateAwaitingDecision || !got.DeliveryHold {
		t.Fatalf("overlay = %+v", got)
	}
}

func (e *scoreEnv) runVersion(t *testing.T) int {
	t.Helper()
	run, err := e.st.GetWorkflow(t.Context(), e.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return run.Version
}

// assertHumanCompleted checks the overlay is closed for good: halted by the
// person finishing the run, hold released, both clocks stopped, the asset free.
func assertHumanCompleted(t *testing.T, e *scoreEnv) model.WorkflowAuto {
	t.Helper()
	a := e.auto(t)
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltHumanCompleted || a.DeliveryHold ||
		a.PausedAt != nil || a.DecisionDeadlineAt != nil {
		t.Fatalf("overlay = %+v", a)
	}
	if _, live, err := e.st.ActiveAutoForAsset(t.Context(), e.run.AssetID); err != nil || live {
		t.Fatalf("the asset is still held by the overlay: live=%v err=%v", live, err)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='human_acceptance'`, e.run.RunID); n != 1 {
		t.Fatalf("human_acceptance decisions = %d, want exactly 1", n)
	}
	return a
}

// A person records acceptance while the overlay waits (MEDIUM) or still works:
// the overlay closes in the same transaction and no longer holds the asset.
func TestRecordAcceptanceClosesALiveOverlay(t *testing.T) {
	for _, state := range []string{"awaiting", "active"} {
		t.Run(state, func(t *testing.T) {
			e := newScoreEnv(t, 0.5, "钓鱼")
			if state == "awaiting" {
				e.toWaiting(t)
			} else {
				e.fullScope(t)
			}
			before := e.auto(t)
			if _, err := e.svc.RecordAcceptance(t.Context(), "human:webui", e.run.RunID, "browser", "passed", "", e.runVersion(t)); err != nil {
				t.Fatal(err)
			}
			a := assertHumanCompleted(t, e)
			if a.AutoVersion != before.AutoVersion+1 {
				t.Fatalf("auto_version = %d, want %d", a.AutoVersion, before.AutoVersion+1)
			}
			if got := auditActions(t, e.svc)["auto.human_completed"]; len(got) != 1 || got[0].Actor != "human:webui" {
				t.Fatalf("audit = %+v", got)
			}
			if len(e.events.named("auto.changed", 1)) == 0 {
				t.Fatal("no auto.changed")
			}
			// Nothing more happens: the driver leaves it alone and a late
			// DecideAuto is refused.
			if err := e.svc.AutoTick(t.Context()); err != nil || e.auto(t).AutoVersion != a.AutoVersion {
				t.Fatalf("tick touched a closed overlay: %v", err)
			}
			if _, _, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "export", "", a.AutoVersion, "k1"); err == nil {
				t.Fatal("a decision was accepted on a closed overlay")
			}
		})
	}
}

// A failed acceptance record is still the person taking the run over.
func TestFailedAcceptanceAlsoClosesTheOverlay(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼")
	e.toWaiting(t)
	if _, err := e.svc.RecordAcceptance(t.Context(), "human:webui", e.run.RunID, "browser", "failed", "画面不对", e.runVersion(t)); err != nil {
		t.Fatal(err)
	}
	assertHumanCompleted(t, e)
}

// A refused acceptance (stale version, bad item, not human) leaves the overlay
// exactly as it was.
func TestRefusedAcceptanceLeavesTheOverlayAlone(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼")
	e.toWaiting(t)
	before := e.auto(t)
	v := e.runVersion(t)
	for name, f := range map[string]func() error{
		"stale version": func() error {
			_, err := e.svc.RecordAcceptance(t.Context(), "human:webui", e.run.RunID, "browser", "passed", "", v+7)
			return err
		},
		"unknown item": func() error {
			_, err := e.svc.RecordAcceptance(t.Context(), "human:webui", e.run.RunID, "nope", "passed", "", v)
			return err
		},
		"auto actor": func() error {
			_, err := e.svc.RecordAcceptance(t.Context(), model.AutoActor, e.run.RunID, "browser", "passed", "", v)
			return err
		},
		"agent actor": func() error {
			_, err := e.svc.RecordAcceptance(t.Context(), "agent:x", e.run.RunID, "browser", "passed", "", v)
			return err
		},
	} {
		if err := f(); err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if got := e.auto(t); got.State != before.State || got.AutoVersion != before.AutoVersion || !got.DeliveryHold {
			t.Fatalf("%s changed the overlay: %+v", name, got)
		}
	}
}

// If the overlay write fails, the acceptance record rolls back with it: the
// two commit together or not at all.
func TestAcceptanceAndOverlayCloseCommitTogether(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼")
	e.toWaiting(t)
	before := e.auto(t)
	v := e.runVersion(t)
	mustExec(t, e.st, `ALTER TABLE auto_decisions RENAME TO auto_decisions_gone`)
	if _, err := e.svc.RecordAcceptance(t.Context(), "human:webui", e.run.RunID, "browser", "passed", "", v); err == nil {
		t.Fatal("acceptance succeeded although the overlay could not be closed")
	}
	mustExec(t, e.st, `ALTER TABLE auto_decisions_gone RENAME TO auto_decisions`)
	if n := count(t, e.svc, `SELECT COUNT(*) FROM acceptance_records WHERE run_id=?`, e.run.RunID); n != 0 {
		t.Fatalf("acceptance rows = %d, want 0 after the rollback", n)
	}
	if got := e.auto(t); got.State != before.State || got.AutoVersion != before.AutoVersion || e.runVersion(t) != v {
		t.Fatalf("rollback left changes: %+v", got)
	}
}

// Acceptance recorded without going through RecordAcceptance (an old row, a
// path that bypasses the hook) is found by the driver.
func TestTickClosesAnOverlayWhoseRunWasAcceptedByAPerson(t *testing.T) {
	for _, state := range []string{"awaiting", "active"} {
		t.Run(state, func(t *testing.T) {
			e := newScoreEnv(t, 0.5, "钓鱼")
			if state == "awaiting" {
				e.toWaiting(t)
			} else {
				e.fullScope(t)
				e.preview(t, false) // a queued preview: the driver has nothing to do yet
			}
			insertAcceptance := func(rev, actor string) {
				t.Helper()
				if err := e.st.InsertAcceptance(t.Context(), model.AcceptanceRecord{RunID: e.run.RunID, ExportRevisionID: rev, ManifestSHA256: "m",
					CheckItem: "browser", Result: "passed", Actor: actor, CreatedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
			}
			// A record for another export revision, or by a non-person, is not
			// this run's acceptance.
			insertAcceptance("rev-other", "human:webui")
			insertAcceptance(e.run.CurrentRevisionID, "system:other")
			v := e.auto(t).AutoVersion
			if err := e.svc.AutoTick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := e.auto(t); got.AutoVersion != v || got.State == model.AutoStateHalted {
				t.Fatalf("closed on the wrong evidence: %+v", e.auto(t))
			}
			insertAcceptance(e.run.CurrentRevisionID, "human:webui")
			if err := e.svc.AutoTick(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertHumanCompleted(t, e)
			if err := e.svc.AutoTick(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertHumanCompleted(t, e) // idempotent: still one decision row
		})
	}
}

// A cancelled run whose overlay was left waiting is closed as cancelled.
func TestTickClosesAWaitingOverlayOfACancelledRun(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼")
	e.toWaiting(t)
	mustExec(t, e.st, `UPDATE workflow_runs SET status = 'cancelled' WHERE run_id = ?`, e.run.RunID)
	if err := e.svc.AutoTick(t.Context()); err != nil {
		t.Fatal(err)
	}
	a := e.auto(t)
	if a.State != model.AutoStateCancelled || a.DeliveryHold || a.DecisionDeadlineAt != nil {
		t.Fatalf("overlay = %+v", a)
	}
	if _, live, _ := e.st.ActiveAutoForAsset(t.Context(), e.run.AssetID); live {
		t.Fatal("asset still held")
	}
}

// acceptBypass inserts a human acceptance for the current export without going
// through RecordAcceptance, so the overlay stays live for the driver to close.
func (e *scoreEnv) acceptBypass(t *testing.T) {
	t.Helper()
	run, err := e.st.GetWorkflow(t.Context(), e.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.InsertAcceptance(t.Context(), model.AcceptanceRecord{RunID: e.run.RunID, ExportRevisionID: run.CurrentRevisionID, ManifestSHA256: "m",
		CheckItem: "browser", Result: "passed", Actor: "human:webui", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func (e *scoreEnv) decisions(t *testing.T, kind string) int {
	t.Helper()
	return count(t, e.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind=?`, e.run.RunID, kind)
}

// The close is decided before the write lock. Replacing the current revision,
// or moving the run off the finished status, without bumping auto_version must
// not still close the overlay from the earlier snapshot.
func TestReconcileDoesNotApplyAStaleFinish(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		args func(*scoreEnv) []any
	}{
		{
			name: "current revision replaced",
			sql:  `UPDATE workflow_runs SET current_revision_id = ? WHERE run_id = ?`,
			args: func(e *scoreEnv) []any { return []any{"rev-newer", e.run.RunID} },
		},
		{
			name: "run left ready_for_acceptance",
			sql:  `UPDATE workflow_runs SET status = ? WHERE run_id = ?`,
			args: func(e *scoreEnv) []any { return []any{model.WorkflowRunning, e.run.RunID} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newScoreEnv(t, 0.5, "钓鱼")
			e.toWaiting(t)
			e.acceptBypass(t)
			before := e.auto(t)
			e.svc.SetBeforeReconcileTx(func() {
				mustExec(t, e.st, tc.sql, tc.args(e)...)
			})
			t.Cleanup(func() { e.svc.SetBeforeReconcileTx(nil) })
			if err := e.svc.AutoTick(t.Context()); err != nil {
				t.Fatal(err)
			}
			got := e.auto(t)
			if got.State != before.State || got.HaltReason != before.HaltReason || got.AutoVersion != before.AutoVersion || !got.DeliveryHold {
				t.Fatalf("stale finish closed the overlay: %+v", got)
			}
			if n := e.decisions(t, model.AutoDecisionHumanAcceptance); n != 0 {
				t.Fatalf("human_acceptance decisions = %d", n)
			}
		})
	}
}

// A waiting overlay of a cancelled run is closed only while that is still
// true at the write. A status change that does not bump auto_version is not
// the cancelled run this tick read.
func TestReconcileDoesNotCancelFromAStaleStatus(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼")
	e.toWaiting(t)
	mustExec(t, e.st, `UPDATE workflow_runs SET status = ? WHERE run_id = ?`, model.WorkflowCancelled, e.run.RunID)
	before := e.auto(t)
	e.svc.SetBeforeReconcileTx(func() {
		mustExec(t, e.st, `UPDATE workflow_runs SET status = ? WHERE run_id = ?`, model.WorkflowRunning, e.run.RunID)
	})
	t.Cleanup(func() { e.svc.SetBeforeReconcileTx(nil) })
	if err := e.svc.AutoTick(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := e.auto(t)
	if got.State != before.State || got.AutoVersion != before.AutoVersion || !got.DeliveryHold {
		t.Fatalf("stale cancel closed the overlay: %+v", got)
	}
	if n := e.decisions(t, model.AutoDecisionCancelled); n != 0 {
		t.Fatalf("workflow_cancelled decisions = %d", n)
	}
}

// auto_version moving either way, while the overlay stays live and the run
// stays finished, means a later write already owns the row. This tick must
// leave that row alone. The following tick, reading the new version, closes it.
func TestReconcileLeavesANewerLiveAutoVersion(t *testing.T) {
	for _, delta := range []int{1, -1} {
		t.Run(fmt.Sprintf("delta %d", delta), func(t *testing.T) {
			e := newScoreEnv(t, 0.5, "钓鱼")
			e.toWaiting(t)
			e.acceptBypass(t)
			before := e.auto(t)
			e.svc.SetBeforeReconcileTx(func() {
				mustExec(t, e.st, `UPDATE workflow_auto SET auto_version = ? WHERE run_id = ?`, before.AutoVersion+delta, e.run.RunID)
			})
			t.Cleanup(func() { e.svc.SetBeforeReconcileTx(nil) })
			if err := e.svc.AutoTick(t.Context()); err != nil {
				t.Fatal(err)
			}
			got := e.auto(t)
			if got.State != model.AutoStateAwaitingDecision || got.AutoVersion != before.AutoVersion+delta || !got.DeliveryHold {
				t.Fatalf("live auto_version %+d was overwritten: %+v", delta, got)
			}
			if n := e.decisions(t, model.AutoDecisionHumanAcceptance); n != 0 {
				t.Fatalf("human_acceptance decisions = %d", n)
			}
			e.svc.SetBeforeReconcileTx(nil)
			if delta < 0 {
				// The stored version must stay positive for the next optimistic write.
				mustExec(t, e.st, `UPDATE workflow_auto SET auto_version = ? WHERE run_id = ?`, before.AutoVersion, e.run.RunID)
			}
			if err := e.svc.AutoTick(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertHumanCompleted(t, e)
		})
	}
}

// RecordAcceptance and the driver tick race for the same overlay: whichever
// commits first, the end state is the same and the close is recorded once.
func TestAcceptanceRacesTheDriverTick(t *testing.T) {
	for i := 0; i < 8; i++ {
		e := newScoreEnv(t, 0.5, "钓鱼")
		e.toWaiting(t)
		v := e.runVersion(t)
		var wg sync.WaitGroup
		errs := make(chan error, 3)
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, err := e.svc.RecordAcceptance(t.Context(), "human:webui", e.run.RunID, "browser", "passed", "", v)
			errs <- err
		}()
		for j := 0; j < 2; j++ {
			go func() {
				defer wg.Done()
				errs <- e.svc.AutoTick(t.Context())
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", i, err)
			}
		}
		// The tick may run before the acceptance exists and find nothing; a
		// second tick after it reconciles whatever the hook did not.
		if err := e.svc.AutoTick(t.Context()); err != nil {
			t.Fatal(err)
		}
		assertHumanCompleted(t, e)
		if n := count(t, e.svc, `SELECT COUNT(*) FROM acceptance_records WHERE run_id=?`, e.run.RunID); n != 1 {
			t.Fatalf("acceptance rows = %d", n)
		}
	}
}
