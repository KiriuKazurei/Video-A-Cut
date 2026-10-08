package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

type mcpTestProber struct {
	media preview.Media
}

func (p *mcpTestProber) ProbeMedia(ctx context.Context, absPath string) (preview.Media, error) {
	return p.media, nil
}

func TestMCPPreviewLifecycle(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "exp1", Role: "exporter"})
	ctx := context.Background()
	root := t.TempDir()
	if err := f.svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}

	f.svc.SetMediaProber(&mcpTestProber{
		media: preview.Media{
			Width:           1280,
			Height:          720,
			FPS:             30,
			FrameCount:      30,
			DurationSeconds: 1.0,
			VideoCodec:      "h264",
			AudioCodec:      "aac",
			SampleRate:      48000,
			Channels:        2,
			AudioInterval:   &preview.AudioInterval{StartPTS: 0, DurationTS: 48000, TimeBase: "1/48000", StartSeconds: 0, DurationSeconds: 1},
		},
	})

	// Setup exported package directory
	pkgDir := filepath.Join(root, "exports", "pkg_mcp")
	if err := os.MkdirAll(filepath.Join(pkgDir, "media"), 0o755); err != nil {
		t.Fatal(err)
	}

	edlContent := `{
		"timeline": {"fps": 30, "sample_rate": 48000},
		"video": [{"src": "media/video.mp4", "in": 0, "out": 1, "timeline_in": 0}],
		"game_audio": [{"src": "media/video.mp4", "in": 0, "out": 1, "timeline_in": 0}],
		"voice": [],
		"music": [],
		"subtitle": []
	}`
	manifestContent := `{
		"schema_version": 1,
		"artifacts": [
			{"kind": "edl", "path": "edl.json"},
			{"kind": "source_video", "path": "media/video.mp4"},
			{"kind": "game_audio", "path": "audio_a1.wav"},
			{"kind": "voice", "path": "audio_a2.wav"},
			{"kind": "music", "path": "audio_a3.wav"}
		]
	}`

	for name, content := range map[string]string{
		"edl.json":               edlContent,
		"delivery-manifest.json": manifestContent,
		"media/video.mp4":        "mcp-test-video-bytes",
		"audio_a1.wav":           "mcp-test-game-wav",
		"audio_a2.wav":           "mcp-test-voice-wav",
		"audio_a3.wav":           "mcp-test-music-wav",
	} {
		p := filepath.Join(pkgDir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	asset := model.Asset{
		AssetID:       "asset-mcp-test",
		Status:        model.AssetStatusExported,
		AgentVisible:  true,
		AllowedAgents: []string{"exporter"},
		Artifacts:     map[string]string{"edl": "exports/pkg_mcp/edl.json", "manifest": "exports/pkg_mcp/delivery-manifest.json"},
	}
	if err := f.svc.CreateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}

	rev := model.Revision{
		RevisionID:    "rev-mcp-1",
		AssetID:       asset.AssetID,
		SchemaVersion: 1,
		PackageRef:    "exports/pkg_mcp",
		EDLSHA256:     "edl-sha-mcp",
		CreatedAt:     time.Now().UTC(),
		CreatedBy:     "agent:exp1",
		Reason:        "export",
	}
	if err := f.st.InsertRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}

	run := model.WorkflowRun{
		RunID:             "run-mcp-test",
		AssetID:           asset.AssetID,
		BaseRevisionID:    rev.RevisionID,
		CurrentRevisionID: rev.RevisionID,
		Status:            model.WorkflowReadyAcceptance,
		Stage:             "acceptance",
		Version:           2,
		ContentMode:       "configured",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := f.st.InsertWorkflow(ctx, run); err != nil {
		t.Fatal(err)
	}

	stg := model.WorkflowStage{
		RunID:            run.RunID,
		TaskID:           "task-exp-mcp",
		Stage:            model.TaskTypeExport,
		InputRevisionID:  "rev-input-0",
		OutputRevisionID: rev.RevisionID,
		Invalidated:      false,
	}
	if err := f.st.InsertStage(ctx, stg); err != nil {
		t.Fatal(err)
	}
	exportTask := model.Task{
		TaskID:    "task-exp-mcp",
		AssetID:   asset.AssetID,
		Type:      model.TaskTypeExport,
		AgentRole: "exporter",
		Status:    model.TaskStatusSucceeded,
		UpdatedAt: time.Now().UTC(),
	}
	if err := f.st.CreateTask(ctx, exportTask); err != nil {
		t.Fatal(err)
	}

	// 1. Report preview_capability via heartbeat tool
	hbArgs := heartbeatArgs{
		PreviewCapability: &preview.Capability{
			ExecutionProtocol: service.PreviewExecutionProtocol,
			ProtocolVersion:   preview.ProtocolVersion,
			Ready:             true,
			Presets: []preview.PresetRef{
				{PresetID: preview.PresetID, SHA256: preview.DefaultPresetSHA256()},
			},
			Checks: []preview.Check{
				{Code: "ffmpeg", Status: "passed", Message: "ready"},
			},
		},
	}
	out, isErr, raw := f.call("exp1", "heartbeat", hbArgs)
	if isErr || out["ok"] != true {
		t.Fatalf("heartbeat with preview_capability failed: %s", raw)
	}

	// Verify capability was registered
	ok, err := f.svc.CheckPreviewCapability(ctx, preview.PresetID)
	if err != nil || !ok {
		t.Fatalf("CheckPreviewCapability = %v, err = %v", ok, err)
	}

	// Start a preview job
	view, created, err := f.svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-mcp-1",
	})
	if err != nil || !created {
		t.Fatalf("StartPreview: %v, created=%v", err, created)
	}

	// 2. Claim preview task via MCP claim_task
	claimOut, isErr, raw := f.call("exp1", "claim_task", claimArgs{RuntimeInstanceID: "preview-test-instance", RequestID: "preview-test-claim"})
	if isErr || claimOut["claimed"] != true {
		t.Fatalf("claim_task failed: %s", raw)
	}
	taskObj, ok := claimOut["task"].(map[string]any)
	if !ok || taskObj["task_id"] != view.TaskID {
		t.Fatalf("expected task %s, got: %+v", view.TaskID, claimOut)
	}

	// 3. Read input via MCP get_task_input
	scopeBytes, err := json.Marshal(claimOut["scope"])
	if err != nil {
		t.Fatal(err)
	}
	var scope executionScopeArgs
	if err = json.Unmarshal(scopeBytes, &scope); err != nil {
		t.Fatal(err)
	}
	inputOut, isErr, raw := f.call("exp1", "get_task_input", scope)
	if isErr {
		t.Fatalf("get_task_input failed: %s", raw)
	}
	if inputOut["input_kind"] != "preview" {
		t.Fatalf("expected input_kind=preview, raw=%s", raw)
	}
	prevObj, ok := inputOut["preview"].(map[string]any)
	if !ok || prevObj["preview_id"] != view.PreviewID {
		t.Fatalf("expected preview input with preview_id %s, got: %+v", view.PreviewID, inputOut)
	}

	// 4. Submit preview via MCP submit_preview
	job, err := f.st.GetPreviewJob(ctx, view.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	previewDir := filepath.Join("previews", job.AssetID, job.RevisionID, job.PreviewID)
	absDir := filepath.Join(root, previewDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mp4Data := []byte("mcp-rendered-mp4-stream")
	mp4Hash := sha256.Sum256(mp4Data)
	mp4SHA := hex.EncodeToString(mp4Hash[:])

	receipt := preview.Receipt{
		SchemaVersion:   1,
		PreviewID:       job.PreviewID,
		TaskID:          job.TaskID,
		InputSHA256:     job.InputSHA256,
		PresetSHA256:    job.PresetSHA256,
		RendererVersion: "1.0.0",
		FFmpegVersion:   "7.1",
	}
	recBytes, _ := json.Marshal(receipt)
	recHash := sha256.Sum256(recBytes)
	recSHA := hex.EncodeToString(recHash[:])

	report := preview.Report{
		SchemaVersion: 1,
		InputSHA256:   job.InputSHA256,
		TargetFrames:  30,
		TargetSamples: 48000,
		Media: preview.Media{
			Width:           1280,
			Height:          720,
			FPS:             30,
			FrameCount:      30,
			DurationSeconds: 1.0,
			VideoCodec:      "h264",
			AudioCodec:      "aac",
			SampleRate:      48000,
			Channels:        2,
			AudioInterval:   &preview.AudioInterval{StartPTS: 0, DurationTS: 48000, TimeBase: "1/48000", StartSeconds: 0, DurationSeconds: 1},
		},
		Checks: []preview.Check{
			{Code: "source_fingerprint", Status: "passed", Message: "verified"},
			{Code: "timeline_frames", Status: "passed", Message: "30 frames"},
			{Code: "audio_alignment", Status: "passed", Message: "aligned"},
			{Code: "subtitles", Status: "passed", Message: "passed"},
			{Code: "media_streams", Status: "passed", Message: "valid"},
		},
	}
	repBytes, _ := json.Marshal(report)
	repHash := sha256.Sum256(repBytes)
	repSHA := hex.EncodeToString(repHash[:])

	manifest := preview.Manifest{
		SchemaVersion: 1,
		PreviewID:     job.PreviewID,
		TaskID:        job.TaskID,
		InputSHA256:   job.InputSHA256,
		PresetSHA256:  job.PresetSHA256,
		Artifacts: []preview.Artifact{
			{Key: "mp4", Kind: "preview", Path: "final.mp4", MIME: "video/mp4", Size: int64(len(mp4Data)), SHA256: mp4SHA},
			{Key: "report", Kind: "render_report", Path: "render-report.json", MIME: "application/json", Size: int64(len(repBytes)), SHA256: repSHA},
			{Key: "receipt", Kind: "receipt", Path: "preview-receipt.json", MIME: "application/json", Size: int64(len(recBytes)), SHA256: recSHA},
		},
	}
	manBytes, _ := json.Marshal(manifest)

	for name, data := range map[string][]byte{
		"final.mp4":             mp4Data,
		"render-report.json":    repBytes,
		"preview-receipt.json":  recBytes,
		"preview-manifest.json": manBytes,
	} {
		if err := os.WriteFile(filepath.Join(absDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	submitOut, isErr, raw := f.call("exp1", "submit_preview", previewArgs{
		RuntimeInstanceID: scope.RuntimeInstanceID, ExecutionID: scope.ExecutionID,
		Generation: scope.Generation, InputSHA256: scope.InputSHA256,
		TaskID:     job.TaskID,
		PreviewDir: filepath.ToSlash(previewDir),
	})
	if isErr || submitOut["submitted"] != true {
		t.Fatalf("submit_preview failed: %s", raw)
	}

	// Verify task is succeeded in control plane
	tk, err := f.st.GetTask(ctx, job.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != model.TaskStatusSucceeded || tk.Progress != 1.0 {
		t.Fatalf("task status = %s, progress = %f", tk.Status, tk.Progress)
	}
}
