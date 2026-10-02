package service_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestWorkflowStartBindsInputAndRejectsASecondActiveRun(t *testing.T) {
	svc := newService(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	edl := []byte(`{"timeline":{"fps":30,"sample_rate":48000},"video":[{"src":"a.mp4","in":0,"out":1,"timeline_in":0}],"game_audio":[],"voice":[],"subtitle":[],"music":[]}`)
	if err := os.WriteFile(filepath.Join(root, "src", "edl.json"), edl, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := svc.CreateAsset(ctx, model.Asset{
		AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
		AllowedAgents: []string{"recognizer", "narrator", "exporter"},
		Artifacts:     map[string]string{"edl": "src/edl.json"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "idem-1", ""); err == nil {
		t.Fatal("missing content mode was accepted")
	}
	run, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "idem-1", "builtin")
	if err != nil {
		t.Fatal(err)
	}
	if run.ContentMode != "builtin" || run.Status != model.WorkflowRunning || run.Stage != model.TaskTypeRecognize {
		t.Fatalf("run = %+v", run)
	}
	view, err := svc.Review(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Scenes == nil || view.Lines == nil || view.Files == nil {
		t.Fatal("empty review must return arrays, not null")
	}
	again, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "idem-1", "builtin")
	if err != nil || again.RunID != run.RunID {
		t.Fatalf("idempotent start = %+v, %v", again, err)
	}
	if _, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "idem-2", "builtin"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second run err = %v, want conflict", err)
	}
	_, stages, err := svc.GetWorkflow(ctx, run.RunID)
	if err != nil || len(stages) != 1 {
		t.Fatalf("stages = %+v, %v", stages, err)
	}
	taskID := stages[0].TaskID
	if _, err := svc.ClaimTask(ctx, "r1", "recognizer", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	in, err := svc.GetTaskInput(ctx, "r1", "recognizer", taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Versioned || in.PackageRef == "src" || in.RevisionID == "" {
		t.Fatalf("input = %+v", in)
	}
	if _, err := svc.GetTaskInput(ctx, "other", "recognizer", taskID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("foreign input err = %v", err)
	}
}

func TestSceneConfirmationOpensSortAndForgedDraftDoesNot(t *testing.T) {
	svc := newService(t)
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	edl := []byte(`{"timeline":{"fps":30,"sample_rate":48000},"video":[{"src":"a.mp4","in":0,"out":1,"timeline_in":0}],"scenes":[{"scene_id":"scn_1","label":"关卡","evidence_frames":["abc"]}],"narration":[{"id":"nar_scn_1","text":"已审批解说","start":0.2,"end":1,"source_scene_label":"Boss"}],"game_audio":[],"voice":[],"subtitle":[],"music":[]}`)
	if err := os.WriteFile(filepath.Join(pkg, "edl.json"), edl, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "delivery-manifest.json"), []byte(`{"schema_version":1,"artifacts":[{"kind":"edl","path":"edl.json"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := svc.CreateAsset(ctx, model.Asset{
		AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
		AllowedAgents: []string{"recognizer", "narrator", "exporter"},
		Artifacts:     map[string]string{"edl": "pkg/edl.json"},
	}); err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "once", "configured")
	if err != nil {
		t.Fatal(err)
	}
	_, stages, err := svc.GetWorkflow(ctx, run.RunID)
	if err != nil || len(stages) != 1 {
		t.Fatalf("stages = %+v, %v", stages, err)
	}
	if _, err := svc.ClaimTask(ctx, "r1", "recognizer", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	recognized := []byte(`{"timeline":{"fps":30,"sample_rate":48000},"video":[{"src":"a.mp4","in":0,"out":1,"timeline_in":0}],"scenes":[{"scene_id":"scn_1","label":"关卡","evidence_frames":["abc"]}],"narration":[{"id":"nar_scn_1","text":"已审批解说","start":0.2,"end":1,"source_scene_label":"Boss"}],"game_audio":[],"voice":[],"subtitle":[],"music":[]}`)
	if err := os.WriteFile(filepath.Join(out, "edl.json"), recognized, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "delivery-manifest.json"), []byte(`{"schema_version":1,"artifacts":[{"kind":"edl","path":"edl.json"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeWorkflowFixture(t, root, "out")
	if err := svc.SubmitDelivery(ctx, "r1", stages[0].TaskID, "out"); err != nil {
		t.Fatal(err)
	}
	run, _, err = svc.GetWorkflow(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Stage != "scene_review" || run.Status != model.WorkflowAwaitingReview {
		t.Fatalf("after recognize = %+v", run)
	}
	confirmed, err := svc.ConfirmScene(ctx, "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "confirmed", "")
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Stage != model.TaskTypeSort || confirmed.Status != model.WorkflowRunning {
		t.Fatalf("after confirm = %+v", confirmed)
	}
	_, stages, err = svc.GetWorkflow(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	foundSort := false
	for _, stg := range stages {
		if stg.Stage == model.TaskTypeSort && stg.InputRevisionID == run.CurrentRevisionID {
			foundSort = true
		}
	}
	if !foundSort {
		t.Fatalf("stages = %+v", stages)
	}
	workflowDeliver(t, svc, root, "recognizer", "sorted")
	workflowDeliver(t, svc, root, "narrator", "narrated")
	if _, err := svc.ApproveNarration(ctx, "human:webui", "clip", service.NarrationDraft{
		Text: "别的解说", Start: 0.2, End: 1, Source: "Boss",
	}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("forged draft err = %v", err)
	}
	hash, err := svc.ApproveNarration(ctx, "human:webui", "clip", service.NarrationDraft{
		Text: "已审批解说", Start: 0.2, End: 1, Source: "Boss",
	})
	if err != nil || hash != service.NarrationDraftHash("已审批解说", 0.2, 1, "Boss") {
		t.Fatalf("hash = %s, %v", hash, err)
	}
}
