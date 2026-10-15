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
