package service_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// deliverStage claims the next task of a role and delivers a package whose EDL is
// the last recognized EDL with mutate applied. The receipt carries the run's
// brief hash, as a real worker's does.
func (e *autoEnv) deliverStage(t *testing.T, role string, mutate func(edl map[string]any)) error {
	t.Helper()
	ctx := t.Context()
	tk, err := e.svc.ClaimTask(ctx, role, role, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := e.svc.GetTaskInput(ctx, role, role, tk.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	e.deliver++
	pkg := fmt.Sprintf("stage%d", e.deliver)
	writeWorkflowFixture(t, e.root, pkg)
	raw, _ := json.Marshal(e.edl)
	var edl map[string]any
	_ = json.Unmarshal(raw, &edl)
	if mutate != nil {
		mutate(edl)
	}
	body, _ := json.Marshal(edl)
	if err := os.WriteFile(filepath.Join(e.root, pkg, "edl.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(map[string]any{"task_id": tk.TaskID, "revision_id": fixed.RevisionID, "content_mode": "configured",
		"profile_sha256": e.sha, "brief_sha256": e.briefSHA})
	if err := os.WriteFile(filepath.Join(e.root, pkg, "worker-receipt.json"), receipt, 0o644); err != nil {
		t.Fatal(err)
	}
	return e.svc.SubmitDelivery(ctx, role, tk.TaskID, pkg)
}

// goodNarration is one valid line for scene scn_1 of the recognized EDL.
func goodNarration(edl map[string]any) {
	edl["narration"] = []any{map[string]any{"id": "nar_scn_1", "text": "团战开始", "start": 0.2, "end": 1.0,
		"source_scene_id": "scn_1", "source_scene_label": "团战A"}}
}

// atDraftReview drives a fresh auto run through human-free scene confirmation
// and the sort and narrate stages, leaving it waiting at draft_review.
func (e *autoEnv) atDraftReview(t *testing.T, brief string) (model.WorkflowRun, int) {
	t.Helper()
	run, view := e.atSceneReview(t, brief, [2]float64{0.9, 0.8})
	after, err := e.svc.AutoConfirmScene(t.Context(), run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deliverStage(t, "recognizer", nil); err != nil { // sort
		t.Fatal(err)
	}
	if err := e.deliverStage(t, "narrator", goodNarration); err != nil {
		t.Fatal(err)
	}
	now, _, err := e.svc.GetWorkflow(t.Context(), after.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if now.Stage != "draft_review" || now.Status != model.WorkflowAwaitingReview {
		t.Fatalf("run is at %s/%s, want draft_review", now.Stage, now.Status)
	}
	v, err := e.svc.GetAuto(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return now, v.AutoVersion
}
