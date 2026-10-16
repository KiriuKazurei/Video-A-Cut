package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// A spent action budget never clears. The driver must stop the run (budget)
// instead of repeating the refused step on every tick until the deadline.
func TestTickHaltsWhenTheActionBudgetIsSpent(t *testing.T) {
	for name, withPreview := range map[string]bool{"before the preview": false, "before scoring": true} {
		t.Run(name, func(t *testing.T) {
			e := newScoreEnv(t, 0.9, "团战")
			e.fullScope(t)
			e.prepPreview(t)
			if withPreview {
				e.preview(t, true)
			}
			mustExec(t, e.st, `UPDATE workflow_auto SET actions_used = 100 WHERE run_id = ?`, e.run.RunID) // fullScope: max_actions is 100
			if err := e.svc.AutoTick(t.Context()); err != nil {
				t.Fatalf("a spent budget is a halt, not a tick error: %v", err)
			}
			a := e.auto(t)
			if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltBudget {
				t.Fatalf("overlay = %+v", a)
			}
			if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='auto_halt'`, e.run.RunID); n != 1 {
				t.Fatalf("halt decisions = %d", n)
			}
			if err := e.svc.AutoTick(t.Context()); err != nil || e.auto(t).AutoVersion != a.AutoVersion {
				t.Fatalf("a halted run moved: %v", err)
			}
		})
	}
}

// An export package that no longer matches its manifest cannot be previewed;
// asking again cannot help, so the run stops with preview_unavailable.
func TestTickHaltsWhenThePreviewCannotStartBecauseThePackageChanged(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	e.prepPreview(t)
	if err := os.Remove(filepath.Join(e.root, "exports", "clip_pkg", "media", "clip.mp4")); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AutoTick(t.Context()); err != nil {
		t.Fatalf("a damaged package is a halt, not a tick error: %v", err)
	}
	a := e.auto(t)
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltPreviewUnavailable {
		t.Fatalf("overlay = %+v", a)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM preview_jobs WHERE run_id=?`, e.run.RunID); n != 0 {
		t.Fatalf("a preview was created: %d", n)
	}
}

// The only preview job of the round is invalidated (the asset was reopened, or
// the export replaced) while the run waits for acceptance. Its request id is
// still taken, so a new start is "reused, nothing written". The driver must
// stop with preview_unavailable on the first tick, not loop until the deadline.
func TestTickHaltsWhenTheRoundsOnlyPreviewWasInvalidated(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	pid := e.preview(t, false)
	if err := e.svc.AutoTick(t.Context()); err != nil { // queued: nothing to do
		t.Fatal(err)
	}
	if a := e.auto(t); a.State != model.AutoStateActive || a.AutoVersion != 1 {
		t.Fatalf("a queued preview moved the run: %+v", a)
	}
	if err := e.st.InvalidatePreviewJob(t.Context(), pid); err != nil {
		t.Fatal(err)
	}
	jobs := count(t, e.svc, `SELECT COUNT(*) FROM preview_jobs WHERE run_id=?`, e.run.RunID)
	if err := e.svc.AutoTick(t.Context()); err != nil {
		t.Fatalf("an invalidated preview is a halt, not a tick error: %v", err)
	}
	a := e.auto(t)
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltPreviewUnavailable {
		t.Fatalf("overlay = %+v", a)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='auto_halt'`, e.run.RunID); n != 1 {
		t.Fatalf("halt decisions = %d", n)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM preview_jobs WHERE run_id=?`, e.run.RunID); n != jobs {
		t.Fatalf("a preview job was created: %d -> %d", jobs, n)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='preview'`, e.run.RunID); n != 0 {
		t.Fatalf("an action was spent: %d", n)
	}
	// Halted: later ticks do nothing at all.
	if err := e.svc.AutoTick(t.Context()); err != nil || e.auto(t).AutoVersion != a.AutoVersion {
		t.Fatalf("a halted run moved: %v", err)
	}
}

// Without the fix this is the spin: every tick reports progress for a step that
// writes nothing. After the halt the overlay is not live and no further steps run.
func TestTickDoesNotSpinOnAnInvalidatedPreview(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.fullScope(t)
	pid := e.preview(t, true) // succeeded, then invalidated
	if err := e.st.InvalidatePreviewJob(t.Context(), pid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.svc.AutoTick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	a := e.auto(t)
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltPreviewUnavailable || a.AutoVersion != 2 {
		t.Fatalf("overlay = %+v", a)
	}
}
