package service_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestAutoViewDeliveryFollowsThePreviewOfTheCurrentExport(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	v, err := e.svc.GetAuto(t.Context(), e.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Delivery.Delivered || !v.Delivery.PackageReady || v.Delivery.PreviewID != "" || v.Delivery.Spec != "720p" ||
		v.Delivery.PreviewPreset != "mp4-720p-v1" || v.Decision != nil || v.DeliveryHoldEnforced {
		t.Fatalf("before any preview: %+v", v)
	}
	pid := e.preview(t, false) // queued
	v, _ = e.svc.GetAuto(t.Context(), e.run.RunID)
	if v.Delivery.Delivered || v.Delivery.PreviewID != pid || v.Delivery.PreviewStatus != model.TaskStatusQueued {
		t.Fatalf("a queued preview is not a delivery: %+v", v.Delivery)
	}
	e.finishPreview(t, pid)
	v, _ = e.svc.GetAuto(t.Context(), e.run.RunID)
	if !v.Delivery.Delivered || !v.Delivery.PreviewCurrent || v.Delivery.PreviewStatus != model.TaskStatusSucceeded {
		t.Fatalf("a finished current preview is the delivery: %+v", v.Delivery)
	}
	// An invalidated preview no longer counts.
	mustExec(t, e.st, `UPDATE preview_jobs SET invalidated = 1 WHERE preview_id = ?`, pid)
	v, _ = e.svc.GetAuto(t.Context(), e.run.RunID)
	if v.Delivery.Delivered || v.Delivery.PreviewID != "" {
		t.Fatalf("an invalidated preview was delivered: %+v", v.Delivery)
	}
}

func TestDecideAutoExportReleasesTheHold(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	pid := e.preview(t, true)
	mustExec(t, e.st, `UPDATE workflow_auto SET state = 'awaiting_user_decision', halt_reason = 'medium', delivery_hold = 1 WHERE run_id = ?`, e.run.RunID)
	e.allowRevise(t)
	a := e.auto(t)
	v, err := e.svc.GetAuto(t.Context(), e.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision == nil || v.Decision.Reason != "medium" || !v.Decision.CanExport || !v.Decision.CanRevise || !v.DeliveryHold || v.DeliveryHoldEnforced {
		t.Fatalf("the waiting run must offer export and revise: %+v %+v", v, v.Decision)
	}
	_, out, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "export", "", a.AutoVersion, "k-export")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != model.AutoStateCompleted || out.DeliveryHold || !out.Delivery.Delivered || out.Delivery.PreviewID != pid || out.Decision != nil {
		t.Fatalf("export = %+v", out)
	}
	// The run itself still waits for the person's acceptance.
	run, _, err := e.svc.GetWorkflow(t.Context(), e.run.RunID)
	if err != nil || run.Status != model.WorkflowReadyAcceptance {
		t.Fatalf("run = %+v %v", run, err)
	}
}

func TestDecideAutoExportNeedsAFinishedPreview(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	e.preview(t, false) // still queued
	mustExec(t, e.st, `UPDATE workflow_auto SET state = 'awaiting_user_decision', halt_reason = 'exhausted', delivery_hold = 1 WHERE run_id = ?`, e.run.RunID)
	a := e.auto(t)
	if _, _, err := e.svc.DecideAuto(t.Context(), "human:webui", e.run.RunID, "export", "", a.AutoVersion, "k"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("export of an unfinished preview: %v", err)
	}
	after := e.auto(t)
	if after.State != model.AutoStateAwaitingDecision || !after.DeliveryHold || after.AutoVersion != a.AutoVersion {
		t.Fatalf("a refused export changed the run: %+v", after)
	}
	v, _ := e.svc.GetAuto(t.Context(), e.run.RunID)
	if v.Decision == nil || v.Decision.CanExport {
		t.Fatalf("the prompt must not offer export: %+v", v.Decision)
	}
	// Revise stays possible, and is withdrawn when the budget is spent.
	e.allowRevise(t)
	if v, _ := e.svc.GetAuto(t.Context(), e.run.RunID); v.Decision == nil || !v.Decision.CanRevise {
		t.Fatalf("revise withdrawn with budget left: %+v", v.Decision)
	}
	mustExec(t, e.st, `UPDATE workflow_auto SET total_rounds = max_total_rounds WHERE run_id = ?`, e.run.RunID)
	v, _ = e.svc.GetAuto(t.Context(), e.run.RunID)
	if v.Decision == nil || v.Decision.CanRevise {
		t.Fatalf("revise offered with no round left: %+v", v.Decision)
	}
}

// allowRevise gives the fixture's scope the TTS budget a real authorization has.
func (e *scoreEnv) allowRevise(t *testing.T) {
	t.Helper()
	a := e.auto(t)
	a.Scope.MaxTTSChars = 1000
	body, _ := json.Marshal(a.Scope)
	mustExec(t, e.st, `UPDATE workflow_auto SET scope_json = ? WHERE run_id = ?`, string(body), e.run.RunID)
}
