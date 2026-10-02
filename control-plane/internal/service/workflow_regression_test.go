package service_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeWorkflowFixture(t *testing.T, root, pkg string) {
	t.Helper()
	dir := filepath.Join(root, pkg)
	if err := os.MkdirAll(filepath.Join(dir, "samples"), 0755); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("frame fixture")
	sum := sha256.Sum256(bytes)
	digest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, "samples", "frame.jpg"), bytes, 0644); err != nil {
		t.Fatal(err)
	}
	edl := map[string]any{"timeline": map[string]any{"fps": 30, "sample_rate": 48000}, "video": []any{map[string]any{"src": "a.mp4", "in": 0, "out": 1, "timeline_in": 0}}, "scenes": []any{map[string]any{"scene_id": "scn_1", "index": 0, "label": "关卡", "method": "vision", "evidence_frames": []string{digest}}}, "narration": []any{map[string]any{"id": "nar_scn_1", "text": "已审批解说", "start": 0.2, "end": 1, "source_scene_id": "scn_1", "source_scene_label": "Boss"}}, "game_audio": []any{}, "voice": []any{}, "subtitle": []any{}, "music": []any{}}
	evidence := map[string]any{"frames": []any{map[string]any{"path": "samples/frame.jpg", "sha256": digest, "clip_index": 0, "timestamp": 0.5}}}
	manifest := map[string]any{"schema_version": 1, "artifacts": []any{map[string]string{"kind": "edl", "path": "edl.json"}, map[string]string{"kind": "sample", "path": "samples/frame.jpg"}, map[string]string{"kind": "evidence_manifest", "path": "samples/evidence-manifest.json"}}}
	for name, data := range map[string]any{"edl.json": edl, "samples/evidence-manifest.json": evidence, "delivery-manifest.json": manifest} {
		body, _ := json.Marshal(data)
		if err := os.WriteFile(filepath.Join(dir, name), body, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
func workflowSetup(t *testing.T) (*service.Service, string, model.WorkflowRun) {
	t.Helper()
	svc := newService(t)
	root := t.TempDir()
	writeWorkflowFixture(t, root, "src")
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateAsset(t.Context(), model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"recognizer", "narrator", "exporter"}, Artifacts: map[string]string{"edl": "src/edl.json"}}); err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartWorkflow(t.Context(), "human:webui", "clip", "", "start", "configured")
	if err != nil {
		t.Fatal(err)
	}
	return svc, root, run
}
func workflowDeliver(t *testing.T, svc *service.Service, root, role, pkg string) model.WorkflowRun {
	t.Helper()
	tk, err := svc.ClaimTask(t.Context(), role, role, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	writeWorkflowFixture(t, root, pkg)
	if err := svc.SubmitDelivery(t.Context(), role, tk.TaskID, pkg); err != nil {
		t.Fatal(err)
	}
	runs, err := svc.ListWorkflows(t.Context(), "clip")
	if err != nil {
		t.Fatal(err)
	}
	return runs[0]
}
func workflowDraftGate(t *testing.T, svc *service.Service, root string) model.WorkflowRun {
	run := workflowDeliver(t, svc, root, "recognizer", "recognized")
	_, err := svc.ConfirmScene(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "confirmed", "")
	if err != nil {
		t.Fatal(err)
	}
	workflowDeliver(t, svc, root, "recognizer", "sorted")
	return workflowDeliver(t, svc, root, "narrator", "narrated")
}
func TestWorkflowIntegrityAndRecovery(t *testing.T) {
	t.Run("snapshot and drift", func(t *testing.T) {
		svc, root, _ := workflowSetup(t)
		tk, err := svc.ClaimTask(t.Context(), "r", "recognizer", time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		in, err := svc.GetTaskInput(t.Context(), "r", "recognizer", tk.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(root, "src", "edl.json"), []byte(`{}`), 0644)
		if _, err := svc.GetTaskInput(t.Context(), "r", "recognizer", tk.TaskID); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(root, filepath.FromSlash(in.EDLPath)), []byte(`{}`), 0644)
		if _, err := svc.GetTaskInput(t.Context(), "r", "recognizer", tk.TaskID); !errors.Is(err, model.ErrConflict) {
			t.Fatalf("drift error=%v", err)
		}
	})
	t.Run("cancel blocks all commits", func(t *testing.T) {
		svc, root, run := workflowSetup(t)
		tk, err := svc.ClaimTask(t.Context(), "r", "recognizer", time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CancelWorkflow(t.Context(), "human:webui", run.RunID, "cancel", run.Version)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.SubmitResult(t.Context(), "r", tk.TaskID, map[string]string{}); err == nil {
			t.Fatal("generic result bypassed gate")
		}
		writeWorkflowFixture(t, root, "late")
		if err := svc.SubmitDelivery(t.Context(), "r", tk.TaskID, "late"); err == nil {
			t.Fatal("late delivery bypassed gate")
		}
	})
	t.Run("lease exhaustion synchronizes", func(t *testing.T) {
		svc, _, run := workflowSetup(t)
		svc.SetMaxAttempts(1)
		if _, err := svc.ClaimTask(t.Context(), "r", "recognizer", time.Now().Add(30*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(80 * time.Millisecond)
		if _, err := svc.RequeueExpiredLeases(t.Context()); err != nil {
			t.Fatal(err)
		}
		run, _, _ = svc.GetWorkflow(t.Context(), run.RunID)
		if run.Status != model.WorkflowFailed {
			t.Fatal(run.Status)
		}
		if _, err := svc.RetryWorkflow(t.Context(), "human:webui", run.RunID, "recognize", "retry", run.Version); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("edit window and evidence", func(t *testing.T) {
		svc, root, _ := workflowSetup(t)
		run := workflowDraftGate(t, svc, root)
		a, b := -1.0, 100.0
		if _, err := svc.EditWorkflow(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "", "", nil, "nar_scn_1", "", &a, &b, ""); err == nil {
			t.Fatal("outside window accepted")
		}
		view, err := svc.Review(t.Context(), run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(root, filepath.FromSlash(view.Revision.PackageRef), "samples", "frame.jpg"), []byte("corrupt"), 0644)
		if _, err := svc.Review(t.Context(), run.RunID); err == nil {
			t.Fatal("corrupt evidence accepted")
		}
	})
	t.Run("legacy approval and revoke", func(t *testing.T) {
		svc, root, _ := workflowSetup(t)
		run := workflowDraftGate(t, svc, root)
		hash, err := svc.ApproveNarration(t.Context(), "human:webui", "clip", service.NarrationDraft{Text: "已审批解说", Start: 0.2, End: 1, Source: "Boss"})
		if err != nil {
			t.Fatal(err)
		}
		run, _, _ = svc.GetWorkflow(t.Context(), run.RunID)
		if run.Stage != model.TaskTypeTTS {
			t.Fatal(run.Stage)
		}
		if err := svc.RevokeNarration(t.Context(), "human:webui", "clip", hash); err != nil {
			t.Fatal(err)
		}
		run, _, _ = svc.GetWorkflow(t.Context(), run.RunID)
		if run.Stage != "draft_review" {
			t.Fatal(run.Stage)
		}
		if _, err := svc.ClaimTask(t.Context(), "n", "narrator", time.Now().Add(time.Minute)); err == nil {
			t.Fatal("revoked TTS claimable")
		}
	})
	t.Run("export edit and quarantine", func(t *testing.T) {
		svc, root, _ := workflowSetup(t)
		run := workflowDraftGate(t, svc, root)
		_, err := svc.ApproveWorkflowNarration(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "nar_scn_1")
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range []string{"tts", "sub", "mix"} {
			workflowDeliver(t, svc, root, "narrator", pkg)
		}
		run = workflowDeliver(t, svc, root, "exporter", "export")
		if _, err := svc.RecordAcceptance(t.Context(), "human:test", run.RunID, "premiere", "failed", " ", run.Version); !errors.Is(err, model.ErrArgument) {
			t.Fatalf("failure without explanation was accepted: %v", err)
		}
		if err := svc.RevokeNarration(t.Context(), "human:webui", "clip", service.NarrationDraftHash("已审批解说", 0.2, 1, "Boss")); !errors.Is(err, model.ErrConflict) {
			t.Fatalf("legacy final-package review must be explicit: %v", err)
		}
		if _, err := svc.RevokeWorkflowNarration(t.Context(), "human:webui", run.RunID, "missing", run.Version); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("unknown narration was accepted: %v", err)
		}
		run, err = svc.RevokeWorkflowNarration(t.Context(), "human:webui", run.RunID, "nar_scn_1", run.Version)
		if err != nil {
			t.Fatal(err)
		}
		run, err = svc.EditWorkflow(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "", "", nil, "nar_scn_1", "修改后的解说", nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.ApproveWorkflowNarration(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "nar_scn_1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ClaimTask(t.Context(), "n", "narrator", time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Join(root, "revisions", "clip", "orphan"), 0755)
		if err := svc.RecoverRevisionFiles(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "revisions", "clip", "orphan")); !os.IsNotExist(err) {
			t.Fatal("orphan not quarantined")
		}
	})
}
