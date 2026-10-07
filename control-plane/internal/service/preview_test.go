package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type testProber struct {
	media preview.Media
	err   error
}

func (p *testProber) ProbeMedia(ctx context.Context, absPath string) (preview.Media, error) {
	if p.err != nil {
		return preview.Media{}, p.err
	}
	return p.media, nil
}

func getTestStore(svc *service.Service) *store.Store {
	v, ok := storesByService.Load(svc)
	if !ok {
		panic("no store for service")
	}
	return v.(*store.Store)
}

func setupPreviewExportFixture(t *testing.T) (*service.Service, string, model.Asset, model.WorkflowRun, model.Revision) {
	t.Helper()
	svc := newService(t)
	root := t.TempDir()
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}

	pkgDir := filepath.Join(root, "exports", "clip_pkg")
	if err := os.MkdirAll(filepath.Join(pkgDir, "media"), 0o755); err != nil {
		t.Fatal(err)
	}

	edlContent := `{
		"timeline": {"fps": 30, "sample_rate": 48000},
		"video": [{"src": "media/clip.mp4", "in": 0, "out": 1, "timeline_in": 0}],
		"game_audio": [{"src": "media/clip.mp4", "in": 0, "out": 1, "timeline_in": 0}],
		"voice": [],
		"music": [],
		"subtitle": []
	}`
	manifestContent := `{
		"schema_version": 1,
		"artifacts": [
			{"kind": "edl", "path": "edl.json"},
			{"kind": "source_video", "path": "media/clip.mp4"},
			{"kind": "game_audio", "path": "audio_a1.wav"},
			{"kind": "voice", "path": "audio_a2.wav"},
			{"kind": "music", "path": "audio_a3.wav"}
		]
	}`

	for name, content := range map[string]string{
		"edl.json":               edlContent,
		"delivery-manifest.json": manifestContent,
		"media/clip.mp4":         "fake-mp4-data-bytes-for-test",
		"audio_a1.wav":           "fake-game-audio-wav-stem",
		"audio_a2.wav":           "fake-voice-wav-stem",
		"audio_a3.wav":           "fake-music-wav-stem",
	} {
		p := filepath.Join(pkgDir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	asset := model.Asset{
		AssetID:       "asset-prev-test",
		Status:        model.AssetStatusExported,
		AgentVisible:  true,
		AllowedAgents: []string{"exporter"},
		Artifacts:     map[string]string{"edl": "exports/clip_pkg/edl.json", "manifest": "exports/clip_pkg/delivery-manifest.json"},
	}
	if err := svc.CreateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}

	rev := model.Revision{
		RevisionID:    "rev-export-1",
		AssetID:       asset.AssetID,
		SchemaVersion: 1,
		PackageRef:    "exports/clip_pkg",
		EDLSHA256:     "edl-sha-1",
		CreatedAt:     time.Now().UTC(),
		CreatedBy:     "agent:exporter",
		Reason:        "export",
	}
	st := getTestStore(svc)
	if err := st.InsertRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}

	run := model.WorkflowRun{
		RunID:             "run-prev-test",
		AssetID:           asset.AssetID,
		BaseRevisionID:    rev.RevisionID,
		CurrentRevisionID: rev.RevisionID,
		Status:            model.WorkflowReadyAcceptance,
		Stage:             "acceptance",
		Version:           3,
		ContentMode:       "configured",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := st.InsertWorkflow(ctx, run); err != nil {
		t.Fatal(err)
	}

	stg := model.WorkflowStage{
		RunID:            run.RunID,
		TaskID:           "task-export-stage",
		Stage:            model.TaskTypeExport,
		InputRevisionID:  "rev-input-0",
		OutputRevisionID: rev.RevisionID,
		Invalidated:      false,
	}
	if err := st.InsertStage(ctx, stg); err != nil {
		t.Fatal(err)
	}

	exportTask := model.Task{
		TaskID:    "task-export-stage",
		AssetID:   asset.AssetID,
		Type:      model.TaskTypeExport,
		AgentRole: "exporter",
		Status:    model.TaskStatusSucceeded,
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.CreateTask(ctx, exportTask); err != nil {
		t.Fatal(err)
	}

	return svc, root, asset, run, rev
}

func registerValidCapability(t *testing.T, svc *service.Service, agentID string) {
	t.Helper()
	ctx := context.Background()
	cap := preview.Capability{
		ProtocolVersion: preview.ProtocolVersion,
		Ready:           true,
		Presets: []preview.PresetRef{
			{PresetID: preview.PresetID, SHA256: preview.DefaultPresetSHA256()},
		},
		Checks: []preview.Check{
			{Code: "ffmpeg", Status: "passed", Message: "ffmpeg ready"},
		},
	}
	if err := svc.ReportPreviewCapability(ctx, agentID, "exporter", cap); err != nil {
		t.Fatalf("ReportPreviewCapability: %v", err)
	}
}

func TestPreviewCapabilityRegistrationAndCheck(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	// Initially no capability
	ok, err := svc.CheckPreviewCapability(ctx, preview.PresetID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no capability initially")
	}

	// Non-exporter role rejected
	err = svc.ReportPreviewCapability(ctx, "agent-1", "narrator", preview.Capability{})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for non-exporter, got: %v", err)
	}

	// Register valid capability
	registerValidCapability(t, svc, "agent-exp-1")
	ok, err = svc.CheckPreviewCapability(ctx, preview.PresetID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected capability to be active")
	}

	// Unknown preset returns false
	ok, err = svc.CheckPreviewCapability(ctx, "mp4-4k-v999")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected unknown preset to be false")
	}
}

func TestStartPreviewValidation(t *testing.T) {
	svc, _, asset, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()

	// 1. Without worker capability, StartPreview fails
	_, _, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-1",
	})
	if !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState when no capability, got %v", err)
	}

	// Register capability
	registerValidCapability(t, svc, "agent-exp-1")

	// 2. Run not found
	_, _, err = svc.StartPreview(ctx, "human:test", "non-existent-run", preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-1",
	})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 3. ExpectedVersion mismatch
	_, _, err = svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version + 1,
		PresetID:        preview.PresetID,
		RequestID:       "req-1",
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("expected ErrConflict for expected_version mismatch, got %v", err)
	}

	// 4. Revision not latest exported
	_, _, err = svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      "wrong-revision",
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-1",
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("expected ErrConflict for wrong revision, got %v", err)
	}

	// 5. Asset locked
	lockedAsset := asset
	lockedAsset.Locked = true
	_ = getTestStore(svc).UpdateAsset(ctx, lockedAsset)
	_, _, err = svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-1",
	})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for locked asset, got %v", err)
	}
	_ = getTestStore(svc).UpdateAsset(ctx, asset) // restore
}

func TestStartPreviewLifecycleAndIdempotency(t *testing.T) {
	svc, _, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	registerValidCapability(t, svc, "agent-exp-1")

	// 1. Initial StartPreview -> creates new job
	view1, created1, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-idem-1",
	})
	if err != nil {
		t.Fatalf("StartPreview: %v", err)
	}
	if !created1 {
		t.Fatal("expected created=true for new job")
	}
	if view1.Status != model.TaskStatusQueued || !view1.IsCurrent || view1.Invalidated {
		t.Fatalf("unexpected view1: %+v", view1)
	}

	// 2. Transport retry with same (run_id, request_id) -> returns existing view, created=false
	view2, created2, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-idem-1",
	})
	if err != nil {
		t.Fatalf("StartPreview retry: %v", err)
	}
	if created2 {
		t.Fatal("expected created=false for idempotent request")
	}
	if view2.PreviewID != view1.PreviewID || view2.TaskID != view1.TaskID {
		t.Fatalf("expected same preview ID, got %s vs %s", view2.PreviewID, view1.PreviewID)
	}

	// 3. New request_id while active job exists with identical input -> reuses active job
	view3, created3, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-idem-2",
	})
	if err != nil {
		t.Fatalf("StartPreview reuse: %v", err)
	}
	if created3 {
		t.Fatal("expected created=false for active reusable job")
	}
	if view3.PreviewID != view1.PreviewID {
		t.Fatalf("expected reused preview ID %s, got %s", view1.PreviewID, view3.PreviewID)
	}

	// 4. ListPreviews
	list, err := svc.ListPreviews(ctx, run.RunID)
	if err != nil {
		t.Fatalf("ListPreviews: %v", err)
	}
	if !list.CanStart || len(list.Checks) != 4 || len(list.Previews) != 1 {
		t.Fatalf("unexpected ListPreviews: %+v", list)
	}

	// 5. GetPreview
	gotView, err := svc.GetPreview(ctx, view1.PreviewID)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if gotView.PreviewID != view1.PreviewID {
		t.Fatalf("expected preview %s, got %s", view1.PreviewID, gotView.PreviewID)
	}
}

func TestClaimPreviewTaskGating(t *testing.T) {
	svc, _, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()

	// Register capability for agent-1
	registerValidCapability(t, svc, "agent-exp-1")

	view, created, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-claim-1",
	})
	if err != nil || !created {
		t.Fatalf("StartPreview: %v, created=%v", err, created)
	}

	// Worker without capability (agent-exp-2) cannot claim
	outcome, err := svc.ClaimForExecution(ctx, "agent-exp-2", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{})
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("ClaimForExecution: %v", err)
	}
	if outcome.Claimed {
		t.Fatal("worker without capability should not claim preview task")
	}

	// Worker with capability (agent-exp-1) CAN claim
	outcome, err = svc.ClaimForExecution(ctx, "agent-exp-1", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{})
	if err != nil {
		t.Fatalf("ClaimForExecution: %v", err)
	}
	if !outcome.Claimed || outcome.Task.TaskID != view.TaskID {
		t.Fatalf("expected claimed task %s, got %+v", view.TaskID, outcome)
	}

	// TaskInput
	in, err := svc.TaskInput(ctx, "agent-exp-1", view.TaskID)
	if err != nil {
		t.Fatalf("TaskInput: %v", err)
	}
	if in.PreviewID != view.PreviewID || in.TaskID != view.TaskID || in.InputSHA256 != view.InputSHA256 {
		t.Fatalf("unexpected TaskInput: %+v", in)
	}

	// Another agent cannot read input
	_, err = svc.TaskInput(ctx, "agent-intruder", view.TaskID)
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func writeCompletePreviewOutput(t *testing.T, root, previewDir string, job model.PreviewJob) {
	t.Helper()
	abs := filepath.Join(root, previewDir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatal(err)
	}

	mp4Data := []byte("dummy-final-mp4-h264-stream-bytes")
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
		"final.mp4":              mp4Data,
		"render-report.json":     repBytes,
		"preview-receipt.json":   recBytes,
		"preview-manifest.json":  manBytes,
	} {
		if err := os.WriteFile(filepath.Join(abs, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSubmitPreviewAndResolveFiles(t *testing.T) {
	svc, root, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	registerValidCapability(t, svc, "agent-exp-1")

	// Set mock prober
	svc.SetMediaProber(&testProber{
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
		},
	})

	// Attach event bus to verify preview_updated event
	bus := events.New()
	svc.SetBus(bus)

	view, _, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-submit-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Claim task
	_, err = svc.ClaimForExecution(ctx, "agent-exp-1", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{})
	if err != nil {
		t.Fatal(err)
	}

	job, err := getTestStore(svc).GetPreviewJob(ctx, view.PreviewID)
	if err != nil {
		t.Fatal(err)
	}

	previewDir := filepath.Join("previews", job.AssetID, job.RevisionID, job.PreviewID)
	writeCompletePreviewOutput(t, root, previewDir, job)

	// Submit preview
	if err := svc.SubmitPreview(ctx, "agent-exp-1", job.TaskID, filepath.ToSlash(previewDir)); err != nil {
		t.Fatalf("SubmitPreview: %v", err)
	}

	// Verify task is succeeded
	tk, err := svc.GetTask(ctx, job.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != model.TaskStatusSucceeded || tk.Progress != 1.0 {
		t.Fatalf("task status = %s, progress = %f", tk.Status, tk.Progress)
	}

	// Verify updated view
	viewAfter, err := svc.GetPreview(ctx, job.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	if viewAfter.Status != model.TaskStatusSucceeded || viewAfter.Media == nil || len(viewAfter.Artifacts) != 3 {
		t.Fatalf("unexpected viewAfter: %+v", viewAfter)
	}

	// Resolve files
	for _, key := range []string{"mp4", "report", "receipt"} {
		f, err := svc.ResolvePreviewFile(ctx, job.PreviewID, key)
		if err != nil {
			t.Fatalf("ResolvePreviewFile(%s): %v", key, err)
		}
		if f.AbsolutePath == "" || f.Size <= 0 || f.SHA256 == "" {
			t.Fatalf("ResolvePreviewFile(%s) invalid: %+v", key, f)
		}
	}

	// Controlled key enforcement
	_, err = svc.ResolvePreviewFile(ctx, job.PreviewID, "unauthorized_key")
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("expected ErrArgument for invalid key, got %v", err)
	}

	// Non-existent preview
	_, err = svc.ResolvePreviewFile(ctx, "non-existent-prev", "mp4")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing preview, got %v", err)
	}
}

func TestCancelPreview(t *testing.T) {
	svc, _, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	registerValidCapability(t, svc, "agent-exp-1")

	view, _, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-cancel-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	cancelledView, err := svc.CancelPreview(ctx, "human:tester", view.PreviewID)
	if err != nil {
		t.Fatalf("CancelPreview: %v", err)
	}
	if cancelledView.Status != model.TaskStatusCancelled {
		t.Fatalf("expected cancelled status, got %s", cancelledView.Status)
	}

	// Cancel again is idempotent
	cancelledView2, err := svc.CancelPreview(ctx, "human:tester", view.PreviewID)
	if err != nil {
		t.Fatalf("CancelPreview again: %v", err)
	}
	if cancelledView2.Status != model.TaskStatusCancelled {
		t.Fatalf("expected cancelled status, got %s", cancelledView2.Status)
	}
}

func TestPreviewInvalidationOnWorkflowCancelAndReopen(t *testing.T) {
	svc, _, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	registerValidCapability(t, svc, "agent-exp-1")

	view, _, err := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-inv-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. CancelWorkflow invalidates preview jobs
	_, err = svc.CancelWorkflow(ctx, "human:admin", run.RunID, "abandoning run", run.Version)
	if err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	job, err := getTestStore(svc).GetPreviewJob(ctx, view.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	if !job.Invalidated {
		t.Fatal("expected preview job to be invalidated after workflow cancellation")
	}

	viewAfterCancel, err := svc.GetPreview(ctx, view.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	if !viewAfterCancel.Invalidated || viewAfterCancel.IsCurrent {
		t.Fatalf("expected invalidated=true, is_current=false, got: %+v", viewAfterCancel)
	}

	// Invalidated task cannot be claimed
	outcome, err := svc.ClaimForExecution(ctx, "agent-exp-1", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{})
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		t.Fatal(err)
	}
	if outcome.Claimed {
		t.Fatal("invalidated preview task must not be claimed")
	}

	// 2. ReopenAsset invalidates all previews for asset
	svc2, root, asset2, run2, rev2 := setupPreviewExportFixture(t)
	registerValidCapability(t, svc2, "agent-exp-1")
	view2, _, err := svc2.StartPreview(ctx, "human:test", run2.RunID, preview.StartRequest{
		RevisionID:      rev2.RevisionID,
		ExpectedVersion: run2.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-reopen-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = root
	_, err = svc2.ReopenAsset(ctx, "human:admin", asset2.AssetID)
	if err != nil {
		t.Fatalf("ReopenAsset: %v", err)
	}
	job2, err := getTestStore(svc2).GetPreviewJob(ctx, view2.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	if !job2.Invalidated {
		t.Fatal("expected preview job to be invalidated after reopen")
	}
}
