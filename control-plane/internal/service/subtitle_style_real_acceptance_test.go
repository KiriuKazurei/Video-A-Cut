package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

// Opt-in: real browser input -> Go REST/SQLite -> MCP worker -> libass MP4 -> browser playback.
func TestASSStyleEditorRealAcceptance(t *testing.T) {
	if os.Getenv("VAC_ASS_EDITOR_ACCEPTANCE") != "1" {
		t.Skip("set VAC_ASS_EDITOR_ACCEPTANCE=1 for full browser acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	svc, root, asset, run, rev := setupPreviewExportFixture(t)
	svc.SetBus(events.New())
	repo, _ := filepath.Abs("../../..")
	out := filepath.Join(repo, ".run-data", "ass-acceptance")
	if err := os.MkdirAll(out, 0700); err != nil {
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
	generate("-f", "lavfi", "-i", "color=c=black:size=640x360:rate=30:duration=8", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", filepath.Join(pkg, "media/clip.mp4"))
	for _, name := range []string{"audio_a1.wav", "audio_a2.wav", "audio_a3.wav"} {
		generate("-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "8", "-c:a", "pcm_s16le", filepath.Join(pkg, name))
	}
	edl := `{"timeline":{"fps":30,"sample_rate":48000},"video":[{"src":"media/clip.mp4","in":0,"out":8,"timeline_in":0}],"game_audio":[],"voice":[],"music":[],"subtitle":[]}`
	if err := os.WriteFile(filepath.Join(pkg, "edl.json"), []byte(edl), 0600); err != nil {
		t.Fatal(err)
	}
	srt := "1\r\n00:00:01,000 --> 00:00:03,000\r\n第一条中文 {\\an8} \\N：这是确认字体颜色描边与长句换行的真实验收字幕。\r\n显式换行保留\r\n\r\n2\r\n00:00:04,000 --> 00:00:06,000\r\n第二条字幕：顶部蓝字黄边\r\n\r\n3\r\n00:00:06,500 --> 00:00:07,900\r\n结束字幕\r\n\r\n"
	addVariantSRT(t, root, rev, srt)
	parent := map[string]string{}
	if err := filepath.WalkDir(pkg, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e == nil {
			parent[path] = hashBytes(b)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	st := getTestStore(svc)
	install := func(a model.Asset, r model.WorkflowRun, v model.Revision, taskID string) error {
		if e := st.InsertRevision(ctx, v); e != nil {
			return e
		}
		if e := st.InsertStage(ctx, model.WorkflowStage{RunID: r.RunID, TaskID: taskID, Stage: model.TaskTypeExport, InputRevisionID: "fixture-input", OutputRevisionID: v.RevisionID}); e != nil {
			return e
		}
		return st.CreateTask(ctx, model.Task{TaskID: taskID, AssetID: a.AssetID, Type: model.TaskTypeExport, AgentRole: "exporter", Status: model.TaskStatusSucceeded, UpdatedAt: time.Now().UTC()})
	}
	second := asset
	second.AssetID = "asset-ass-B"
	if e := svc.CreateAsset(ctx, second); e != nil {
		t.Fatal(e)
	}
	secondRun := run
	secondRun.RunID = "run-ass-B"
	secondRun.AssetID = second.AssetID
	secondRun.CurrentRevisionID = "rev-ass-B"
	secondRun.BaseRevisionID = "rev-ass-B"
	secondRev := rev
	secondRev.RevisionID = secondRun.CurrentRevisionID
	secondRev.AssetID = second.AssetID
	if e := st.InsertWorkflow(ctx, secondRun); e != nil {
		t.Fatal(e)
	}
	if e := install(second, secondRun, secondRev, "task-ass-B"); e != nil {
		t.Fatal(e)
	}
	token := strings.Repeat("a", 40)
	tokenSHA := sha256.Sum256([]byte(token))
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.New([]mcpserver.Agent{{ID: "agent-ass-acceptance", Role: "exporter", TokenSHA256: hex.EncodeToString(tokenSHA[:])}}, svc, time.Minute))
	mux.Handle("/api/", api.New(svc).Handler())
	mux.HandleFunc("POST /acceptance-fixture/revision", func(w http.ResponseWriter, r *http.Request) {
		current, e := st.GetWorkflow(ctx, run.RunID)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		fresh := rev
		fresh.RevisionID = "rev-export-2"
		fresh.CreatedAt = time.Now().UTC()
		if e = install(asset, current, fresh, "task-ass-rev2"); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		current.Version++
		current.CurrentRevisionID = fresh.RevisionID
		current.UpdatedAt = time.Now().UTC()
		if e = st.UpdateWorkflow(ctx, current); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"revision_id": fresh.RevisionID})
	})
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(out, "webui"))))
	server := httptest.NewServer(mux)
	defer server.Close()
	cfg := map[string]any{"mcp_url": server.URL + "/mcp", "token_env": "VAC_ASS_ACCEPTANCE_TOKEN", "delivery_root": root,
		"cli_path": filepath.Join(repo, "workers/node/timeline-cli/src/cli.mjs"), "adapters_path": filepath.Join(repo, "workers/node/timeline-cli/adapters/index.mjs"),
		"heartbeat_interval_ms": 200, "poll_interval_ms": 100, "task_timeout_ms": 60000}
	raw, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(root, "worker.json")
	if e := os.WriteFile(cfgPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	worker := exec.CommandContext(workerCtx, "node", filepath.Join(repo, "workers/node/export-worker/src/main.mjs"), "--config", cfgPath)
	worker.Env = append(os.Environ(), "VAC_ASS_ACCEPTANCE_TOKEN="+token)
	log, e := os.Create(filepath.Join(out, "worker.log"))
	if e != nil {
		t.Fatal(e)
	}
	worker.Stdout = log
	worker.Stderr = log
	if e = worker.Start(); e != nil {
		log.Close()
		t.Fatal(e)
	}
	workerDone := make(chan error, 1)
	go func() { workerDone <- worker.Wait() }()
	t.Cleanup(func() {
		stopWorker()
		select {
		case <-workerDone:
		case <-time.After(5 * time.Second):
			t.Error("worker did not exit")
		}
		_ = log.Close()
	})
	for attempt := 0; attempt < 100; attempt++ {
		fonts, e := svc.SubtitleFonts(ctx)
		if e == nil && len(fonts) > 0 {
			break
		}
		if attempt == 99 {
			t.Fatal("worker font catalogue did not become ready", e)
		}
		time.Sleep(100 * time.Millisecond)
	}
	browser := exec.CommandContext(ctx, "node", filepath.Join(repo, "scripts/verify-ass-editor-chain.mjs"), server.URL, run.RunID, asset.AssetID, second.AssetID)
	browser.Env = append(os.Environ(), "VAC_TEST_OUTPUT="+filepath.Join(out, "browser"))
	if b, e := browser.CombinedOutput(); e != nil {
		t.Fatalf("browser: %v %s; logs: %s", e, b, out)
	} else {
		t.Log(string(b))
	}
	var browserResult struct {
		Primary struct {
			VariantID string `json:"variant_id"`
			PreviewID string `json:"preview_id"`
		} `json:"primary"`
	}
	b, e := os.ReadFile(filepath.Join(out, "browser/results.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &browserResult); e != nil {
		t.Fatal(e)
	}
	if browserResult.Primary.VariantID == "" || browserResult.Primary.PreviewID == "" {
		t.Fatal("browser did not produce ASS/MP4 identities")
	}
	variant, e := st.GetDeliveryVariantJob(ctx, browserResult.Primary.VariantID)
	if e != nil {
		t.Fatal(e)
	}
	previewJob, e := st.GetPreviewJob(ctx, browserResult.Primary.PreviewID)
	if e != nil {
		t.Fatal(e)
	}
	for name, source := range map[string]string{
		"styled.ass":           filepath.Join(root, filepath.FromSlash(variant.OutputDir), "subtitles.ass"),
		"ass-style.json":       filepath.Join(root, filepath.FromSlash(variant.OutputDir), "ass-style.json"),
		"styled.mp4":           filepath.Join(root, filepath.FromSlash(previewJob.OutputDir), "final.mp4"),
		"preview-receipt.json": filepath.Join(root, filepath.FromSlash(previewJob.OutputDir), "preview-receipt.json"),
		"render-report.json":   filepath.Join(root, filepath.FromSlash(previewJob.OutputDir), "render-report.json")} {
		bytes, e := os.ReadFile(source)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(out, name), bytes, 0600); e != nil {
			t.Fatal(e)
		}
	}
	_ = os.WriteFile(filepath.Join(out, "ass-input.json"), []byte(variant.InputJSON), 0600)
	_ = os.WriteFile(filepath.Join(out, "mp4-input.json"), []byte(previewJob.InputJSON), 0600)
	for path, hash := range parent {
		bytes, e := os.ReadFile(path)
		if e != nil || hashBytes(bytes) != hash {
			t.Fatal("original delivery changed", path, e)
		}
	}
	after, e := svc.GetAsset(ctx, asset.AssetID)
	if e != nil || after.Status != asset.Status || after.Artifacts["edl"] != asset.Artifacts["edl"] {
		t.Fatal("original asset changed", e)
	}
	_ = os.WriteFile(filepath.Join(out, "parent-integrity.json"), []byte(`{"original_delivery_hashes_unchanged":true,"original_asset_unchanged":true}`), 0600)
	pixels := exec.CommandContext(ctx, "node", filepath.Join(repo, "scripts/verify-ass-editor-frames.mjs"), out)
	if b, e := pixels.CombinedOutput(); e != nil {
		t.Fatalf("frames: %v %s", e, b)
	} else {
		t.Log(string(b))
	}
}
