package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type apiTestProber struct {
	media preview.Media
}

func (p *apiTestProber) ProbeMedia(ctx context.Context, absPath string) (preview.Media, error) {
	return p.media, nil
}

func setupAPIPreviewFixture(t *testing.T) (*Server, string, *store.Store, model.Asset, model.WorkflowRun, model.Revision) {
	t.Helper()
	workspace, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	testParent := filepath.Join(workspace, ".run-data", "delivery-tests")
	if err := os.MkdirAll(testParent, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(testParent, "case-preview-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rel, err := filepath.Rel(testParent, root)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			_ = os.RemoveAll(root)
		}
	})

	st, err := store.Open(filepath.Join(t.TempDir(), "api-preview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	svc := service.New(st)
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	srv := New(svc)
	ctx := context.Background()

	// Setup media prober
	srv.svc.SetMediaProber(&apiTestProber{
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

	// Setup exported package directory
	pkgDir := filepath.Join(root, "exports", "pkg_1")
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
		"media/video.mp4":        "test-video-stream-bytes-12345",
		"audio_a1.wav":           "test-game-audio-wav",
		"audio_a2.wav":           "test-voice-wav",
		"audio_a3.wav":           "test-music-wav",
	} {
		p := filepath.Join(pkgDir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	asset := model.Asset{
		AssetID:       "asset-api-test",
		Status:        model.AssetStatusExported,
		AgentVisible:  true,
		AllowedAgents: []string{"exporter"},
		Artifacts:     map[string]string{"edl": "exports/pkg_1/edl.json", "manifest": "exports/pkg_1/delivery-manifest.json"},
	}
	if err := srv.svc.CreateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}

	rev := model.Revision{
		RevisionID:    "rev-api-1",
		AssetID:       asset.AssetID,
		SchemaVersion: 1,
		PackageRef:    "exports/pkg_1",
		EDLSHA256:     "edl-sha-api",
		CreatedAt:     time.Now().UTC(),
		CreatedBy:     "agent:exporter",
		Reason:        "export",
	}
	if err := st.InsertRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}

	run := model.WorkflowRun{
		RunID:             "run-api-test",
		AssetID:           asset.AssetID,
		BaseRevisionID:    rev.RevisionID,
		CurrentRevisionID: rev.RevisionID,
		Status:            model.WorkflowReadyAcceptance,
		Stage:             "acceptance",
		Version:           5,
		ContentMode:       "configured",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := st.InsertWorkflow(ctx, run); err != nil {
		t.Fatal(err)
	}

	stg := model.WorkflowStage{
		RunID:            run.RunID,
		TaskID:           "task-exp-stage",
		Stage:            model.TaskTypeExport,
		InputRevisionID:  "rev-input-0",
		OutputRevisionID: rev.RevisionID,
		Invalidated:      false,
	}
	if err := st.InsertStage(ctx, stg); err != nil {
		t.Fatal(err)
	}
	exportTask := model.Task{
		TaskID:    "task-exp-stage",
		AssetID:   asset.AssetID,
		Type:      model.TaskTypeExport,
		AgentRole: "exporter",
		Status:    model.TaskStatusSucceeded,
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.CreateTask(ctx, exportTask); err != nil {
		t.Fatal(err)
	}

	// Register exporter capability
	cap := preview.Capability{
		ExecutionProtocol: service.PreviewExecutionProtocol,
		ProtocolVersion:   preview.ProtocolVersion,
		Ready:             true,
		Presets: []preview.PresetRef{
			{PresetID: preview.PresetID, SHA256: preview.DefaultPresetSHA256()},
		},
		Checks: []preview.Check{
			{Code: "ffmpeg", Status: "passed", Message: "ready"},
		},
	}
	if err := srv.svc.ReportPreviewCapability(ctx, "agent-exp-api", "exporter", cap); err != nil {
		t.Fatal(err)
	}

	return srv, root, st, asset, run, rev
}

func doAPIReq(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = "127.0.0.1"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func writeOutputForJob(t *testing.T, root, previewDir string, job model.PreviewJob) {
	t.Helper()
	abs := filepath.Join(root, previewDir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatal(err)
	}

	mp4Data := []byte("mp4-binary-content-playable-stream")
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
		"final.mp4":             mp4Data,
		"render-report.json":    repBytes,
		"preview-receipt.json":  recBytes,
		"preview-manifest.json": manBytes,
	} {
		if err := os.WriteFile(filepath.Join(abs, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAPIPreviewEndToEnd(t *testing.T) {
	srv, root, st, _, run, rev := setupAPIPreviewFixture(t)
	ctx := context.Background()

	// 1. POST /api/workflows/{run}/previews -> 202 Accepted
	startReq := preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-api-start-1",
	}
	startBody, _ := json.Marshal(startReq)
	rec := doAPIReq(srv, http.MethodPost, fmt.Sprintf("/api/workflows/%s/previews", run.RunID), string(startBody))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST previews = %d %s, want 202", rec.Code, rec.Body.String())
	}
	var view1 preview.View
	if err := json.Unmarshal(rec.Body.Bytes(), &view1); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view1.PreviewID == "" || view1.TaskID == "" || view1.Status != model.TaskStatusQueued {
		t.Fatalf("unexpected view: %+v", view1)
	}

	// 2. Transport retry with same request_id -> 200 OK
	recRetry := doAPIReq(srv, http.MethodPost, fmt.Sprintf("/api/workflows/%s/previews", run.RunID), string(startBody))
	if recRetry.Code != http.StatusOK {
		t.Fatalf("POST previews retry = %d %s, want 200", recRetry.Code, recRetry.Body.String())
	}
	var viewRetry preview.View
	_ = json.Unmarshal(recRetry.Body.Bytes(), &viewRetry)
	if viewRetry.PreviewID != view1.PreviewID {
		t.Fatalf("expected same preview ID on retry, got %s vs %s", viewRetry.PreviewID, view1.PreviewID)
	}

	// 3. GET /api/workflows/{run}/previews -> 200 OK
	recList := doAPIReq(srv, http.MethodGet, fmt.Sprintf("/api/workflows/%s/previews", run.RunID), "")
	if recList.Code != http.StatusOK {
		t.Fatalf("GET previews = %d %s, want 200", recList.Code, recList.Body.String())
	}
	var list preview.List
	if err := json.Unmarshal(recList.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !list.CanStart || len(list.Previews) != 1 || list.RevisionID != rev.RevisionID {
		t.Fatalf("unexpected preview list: %+v", list)
	}

	// 4. GET /api/previews/{id} -> 200 OK
	recGet := doAPIReq(srv, http.MethodGet, fmt.Sprintf("/api/previews/%s", view1.PreviewID), "")
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET preview = %d %s, want 200", recGet.Code, recGet.Body.String())
	}
	var viewGet preview.View
	_ = json.Unmarshal(recGet.Body.Bytes(), &viewGet)
	if viewGet.PreviewID != view1.PreviewID {
		t.Fatalf("expected preview %s, got %s", view1.PreviewID, viewGet.PreviewID)
	}

	// 5. POST /api/previews/{id}/cancel -> 200 OK
	recCancel := doAPIReq(srv, http.MethodPost, fmt.Sprintf("/api/previews/%s/cancel", view1.PreviewID), "{}")
	if recCancel.Code != http.StatusOK {
		t.Fatalf("POST cancel = %d %s, want 200", recCancel.Code, recCancel.Body.String())
	}
	var viewCancel preview.View
	_ = json.Unmarshal(recCancel.Body.Bytes(), &viewCancel)
	if viewCancel.Status != model.TaskStatusCancelled {
		t.Fatalf("expected cancelled status, got %s", viewCancel.Status)
	}

	// 6. Submit a preview output and test file endpoints
	// Start a new preview task
	startReq2 := preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-api-start-2",
	}
	startBody2, _ := json.Marshal(startReq2)
	rec2 := doAPIReq(srv, http.MethodPost, fmt.Sprintf("/api/workflows/%s/previews", run.RunID), string(startBody2))
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("POST previews 2 = %d %s, want 202", rec2.Code, rec2.Body.String())
	}
	var view2 preview.View
	_ = json.Unmarshal(rec2.Body.Bytes(), &view2)

	// Claim and submit
	claim, err := srv.svc.ClaimForExecution(ctx, "agent-exp-api", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{RuntimeInstanceID: "api-test-instance", RequestID: "api-test-preview-claim"})
	if err != nil {
		t.Fatalf("ClaimForExecution: %v", err)
	}

	job2, err := st.GetPreviewJob(ctx, view2.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	previewDir := filepath.Join("previews", job2.AssetID, job2.RevisionID, job2.PreviewID)
	writeOutputForJob(t, root, previewDir, job2)
	if err := srv.svc.SubmitPreviewScoped(ctx, claim.Scope, filepath.ToSlash(previewDir)); err != nil {
		t.Fatalf("SubmitPreview: %v", err)
	}

	// 7. GET /api/previews/{id}/files/mp4 inline playback
	recMP4 := doAPIReq(srv, http.MethodGet, fmt.Sprintf("/api/previews/%s/files/mp4", view2.PreviewID), "")
	if recMP4.Code != http.StatusOK {
		t.Fatalf("GET mp4 = %d %s, want 200", recMP4.Code, recMP4.Body.String())
	}
	if recMP4.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("content type = %s, want video/mp4", recMP4.Header().Get("Content-Type"))
	}
	if disp := recMP4.Header().Get("Content-Disposition"); strings.Contains(disp, "attachment") {
		t.Fatalf("inline mp4 should not have attachment disposition, got: %s", disp)
	}

	// Range request: 206 Partial Content
	recRange := httptest.NewRecorder()
	reqRange := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/previews/%s/files/mp4", view2.PreviewID), nil)
	reqRange.Host = "127.0.0.1"
	reqRange.Header.Set("Range", "bytes=0-9")
	srv.Handler().ServeHTTP(recRange, reqRange)
	if recRange.Code != http.StatusPartialContent {
		t.Fatalf("Range request = %d %s, want 206", recRange.Code, recRange.Body.String())
	}
	if recRange.Body.Len() != 10 {
		t.Fatalf("range body len = %d, want 10", recRange.Body.Len())
	}

	// ?download=1 downloads as attachment
	recDL := doAPIReq(srv, http.MethodGet, fmt.Sprintf("/api/previews/%s/files/mp4?download=1", view2.PreviewID), "")
	if recDL.Code != http.StatusOK {
		t.Fatalf("GET download = %d, want 200", recDL.Code)
	}
	if disp := recDL.Header().Get("Content-Disposition"); !strings.Contains(disp, "attachment") {
		t.Fatalf("expected attachment disposition for download=1, got: %s", disp)
	}

	// Report and receipt are attachments
	recReport := doAPIReq(srv, http.MethodGet, fmt.Sprintf("/api/previews/%s/files/report", view2.PreviewID), "")
	if recReport.Code != http.StatusOK {
		t.Fatalf("GET report = %d, want 200", recReport.Code)
	}
	if disp := recReport.Header().Get("Content-Disposition"); !strings.Contains(disp, "attachment") {
		t.Fatalf("expected attachment for report, got: %s", disp)
	}

	// Invalid key
	recBadKey := doAPIReq(srv, http.MethodGet, fmt.Sprintf("/api/previews/%s/files/unknown", view2.PreviewID), "")
	if recBadKey.Code != http.StatusBadRequest {
		t.Fatalf("GET bad key = %d, want 400", recBadKey.Code)
	}
}

func TestAPIPreviewErrorResponses(t *testing.T) {
	srv, _, _, _, run, rev := setupAPIPreviewFixture(t)

	// 404 Run not found
	startReq := preview.StartRequest{
		RevisionID:      rev.RevisionID,
		ExpectedVersion: run.Version,
		PresetID:        preview.PresetID,
		RequestID:       "req-err-1",
	}
	body, _ := json.Marshal(startReq)
	rec := doAPIReq(srv, http.MethodPost, "/api/workflows/non-existent-run/previews", string(body))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	// 409 Version mismatch
	startReq.ExpectedVersion = 999
	body, _ = json.Marshal(startReq)
	rec = doAPIReq(srv, http.MethodPost, fmt.Sprintf("/api/workflows/%s/previews", run.RunID), string(body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}

	// 400 Malformed JSON
	rec = doAPIReq(srv, http.MethodPost, fmt.Sprintf("/api/workflows/%s/previews", run.RunID), "{invalid-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
