package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/mcpserver"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

// Uses the real REST/MCP handlers, Node worker, renderer and production ffprobe.
func TestPreviewRealWorkerChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	svc, root, asset, run, rev := setupPreviewExportFixture(t)
	svc.SetBus(events.New())
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, filepath.FromSlash(rev.PackageRef))
	generate := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "ffmpeg", append([]string{"-v", "error", "-y"}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("ffmpeg: %v %s", e, b)
		}
	}
	src := "media/游戏源 & clip.mp4"
	generate("-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30:duration=4", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", filepath.Join(pkg, filepath.FromSlash(src)))
	for i, name := range []string{"audio_a1.wav", "audio_a2.wav", "audio_a3.wav"} {
		freq := []string{"440", "880", "220"}[i]
		generate("-f", "lavfi", "-i", "sine=frequency="+freq+":sample_rate=48000:duration=6", "-ac", "2", "-c:a", "pcm_s16le", filepath.Join(pkg, name))
	}
	edl := map[string]any{"timeline": map[string]int{"fps": 30, "sample_rate": 48000}, "video": []map[string]any{{"src": src, "in": 0, "out": 2, "timeline_in": 0}, {"src": src, "in": 1, "out": 4, "timeline_in": 3}}, "game_audio": []any{}, "voice": []any{}, "music": []any{}, "subtitle": []any{}}
	edlBytes, _ := json.Marshal(edl)
	if err = os.WriteFile(filepath.Join(pkg, "edl.json"), edlBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(pkg, "subtitles.srt"), []byte("1\n00:00:00,200 --> 00:00:05,500\n中文预览 & 三轨混音\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"schema_version": 1, "artifacts": []map[string]string{{"kind": "edl", "path": "edl.json"}, {"kind": "source_video", "path": src}, {"kind": "game_audio", "path": "audio_a1.wav"}, {"kind": "voice", "path": "audio_a2.wav"}, {"kind": "music", "path": "audio_a3.wav"}, {"kind": "subtitles", "path": "subtitles.srt"}}}
	manBytes, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(pkg, "delivery-manifest.json"), manBytes, 0600); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 40)
	h := sha256.Sum256([]byte(token))
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.New([]mcpserver.Agent{{ID: "agent-real-exporter", Role: "exporter", TokenSHA256: hex.EncodeToString(h[:])}}, svc, time.Minute))
	mux.Handle("/api/", api.New(svc).Handler())
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(repo, "webui", "dist"))))
	server := httptest.NewServer(mux)
	defer server.Close()
	registerValidCapability(t, svc, "agent-real-exporter")
	body, _ := json.Marshal(preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, PresetID: preview.PresetID, RequestID: "real-chain"})
	resp, err := http.Post(server.URL+"/api/workflows/"+run.RunID+"/previews", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("REST start: %d %s %v", resp.StatusCode, raw, err)
	}
	var view preview.View
	if err = json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"mcp_url": server.URL + "/mcp", "token_env": "VAC_CHAIN_TEST_TOKEN", "delivery_root": root, "cli_path": filepath.Join(repo, "workers/node/timeline-cli/src/cli.mjs"), "adapters_path": filepath.Join(repo, "workers/node/timeline-cli/adapters/index.mjs"), "heartbeat_interval_ms": 200, "poll_interval_ms": 50, "task_timeout_ms": 60000}
	cfgBytes, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(root, "worker.json")
	if err = os.WriteFile(cfgPath, cfgBytes, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(repo, "workers/node/export-worker/src/main.mjs"), "--config", cfgPath, "--once")
	cmd.Env = append(os.Environ(), "VAC_CHAIN_TEST_TOKEN="+token)
	workerOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worker: %v %s", err, workerOutput)
	}
	t.Log(string(workerOutput))
	done, err := svc.GetPreview(ctx, view.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != model.TaskStatusSucceeded || done.Media == nil || done.Media.FrameCount != 180 || done.Media.FPS != 30 || done.Media.SampleRate != 48000 || done.Media.VideoCodec != "h264" || done.Media.AudioCodec != "aac" {
		t.Fatalf("real preview: %+v", done)
	}
	after, err := svc.GetAsset(ctx, asset.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Artifacts["edl"] != asset.Artifacts["edl"] || after.Status != asset.Status {
		t.Fatal("preview modified original delivery asset")
	}
	job, err := getTestStore(svc).GetPreviewJob(ctx, view.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(repo, ".run-data/mp4-preview-real-chain/latest")
	if selected := os.Getenv("VAC_PREVIEW_TEST_OUTPUT"); selected != "" {
		selected = filepath.Clean(filepath.FromSlash(selected))
		if !filepath.IsLocal(selected) || !strings.HasPrefix(filepath.ToSlash(selected), ".run-data/") {
			t.Fatal("VAC_PREVIEW_TEST_OUTPUT must be a repository-local .run-data path")
		}
		out = filepath.Join(repo, selected)
	}
	if err = os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"final.mp4", "render-report.json", "preview-receipt.json", "preview-manifest.json"} {
		data, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(job.OutputDir), name))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(out, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	result, _ := json.MarshalIndent(done, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "result.json"), result, 0600); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/previews/"+view.PreviewID+"/files/mp4", nil)
	req.Header.Set("Range", "bytes=0-31")
	rangeResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, rangeResp.Body)
	rangeResp.Body.Close()
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatal("MP4 Range did not return 206")
	}
	if os.Getenv("VAC_PREVIEW_BROWSER_CHECK") == "1" {
		browser := exec.CommandContext(ctx, "node", filepath.Join(repo, "scripts/verify-mp4-preview-live.mjs"), server.URL, run.RunID)
		browser.Env = append(os.Environ(), "VAC_TEST_OUTPUT="+filepath.Join(out, "browser"))
		if b, e := browser.CombinedOutput(); e != nil {
			t.Fatalf("real browser: %v %s", e, b)
		} else {
			t.Log(string(b))
		}
	}
}
