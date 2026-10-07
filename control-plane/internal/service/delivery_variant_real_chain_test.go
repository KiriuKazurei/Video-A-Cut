package service_test

import (
	"archive/zip"
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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/mcpserver"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// Real REST + MCP + worker + registered downloads + FFmpeg/libass rendering.
func TestVariantRealWorkerChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	svc, root, asset, run, rev := setupPreviewExportFixture(t)
	svc.SetBus(events.New())
	repo, _ := filepath.Abs("../../..")
	pkg := filepath.Join(root, filepath.FromSlash(rev.PackageRef))
	generate := func(dir string, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, "ffmpeg", append([]string{"-v", "error", "-y"}, args...)...)
		cmd.Dir = dir
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("ffmpeg: %v %s", e, b)
		}
		return b
	}
	generate(pkg, "-f", "lavfi", "-i", "color=c=black:size=1280x720:rate=30:duration=1", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "media/clip.mp4")
	for _, name := range []string{"audio_a1.wav", "audio_a2.wav", "audio_a3.wav"} {
		generate(pkg, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1", "-ac", "2", "-c:a", "pcm_s16le", name)
	}
	srt := "1\r\n00:00:00,205 --> 00:00:00,795\r\n中文样式字幕, {\\an8} \\N。这是用于确认长句自动换行与字幕安全区的验收文本。这是用于确认长句自动换行与字幕安全区的验收文本。\r\n第二行保留\r\n\r\n2\r\n00:00:00,805 --> 00:00:00,995\r\n结束字幕\r\n\r\n"
	addVariantSRT(t, root, rev, srt)
	var parent map[string]string = map[string]string{}
	_ = filepath.WalkDir(pkg, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			parent[p] = hashBytes(b)
		}
		return nil
	})
	token := strings.Repeat("a", 40)
	h := sha256.Sum256([]byte(token))
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.New([]mcpserver.Agent{{ID: "agent-ass-exporter", Role: "exporter", TokenSHA256: hex.EncodeToString(h[:])}}, svc, time.Minute))
	mux.Handle("/api/", api.New(svc).Handler())
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(repo, "webui/dist"))))
	server := httptest.NewServer(mux)
	defer server.Close()
	registerVariantCapability(t, svc, "agent-ass-exporter")
	body, _ := json.Marshal(variantRequest(run, rev, "real-ass-chain"))
	resp, err := http.Post(server.URL+"/api/workflows/"+run.RunID+"/delivery-variants", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("start %d %s", resp.StatusCode, raw)
	}
	var view dv.View
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"mcp_url": server.URL + "/mcp", "token_env": "VAC_ASS_TEST_TOKEN", "delivery_root": root, "cli_path": filepath.Join(repo, "workers/node/timeline-cli/src/cli.mjs"), "adapters_path": filepath.Join(repo, "workers/node/timeline-cli/adapters/index.mjs"), "heartbeat_interval_ms": 200, "poll_interval_ms": 50, "task_timeout_ms": 60000}
	raw, _ = json.Marshal(cfg)
	cfgPath := filepath.Join(root, "worker.json")
	_ = os.WriteFile(cfgPath, raw, 0600)
	cmd := exec.CommandContext(ctx, "node", filepath.Join(repo, "workers/node/export-worker/src/main.mjs"), "--config", cfgPath, "--once")
	cmd.Env = append(os.Environ(), "VAC_ASS_TEST_TOKEN="+token)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("worker %v %s", e, b)
	} else {
		t.Log(string(b))
	}
	done, err := svc.GetDeliveryVariant(ctx, view.VariantID)
	if err != nil || done.Status != model.TaskStatusSucceeded || !done.IsCurrent {
		t.Fatal(done, err)
	}
	after, err := svc.GetAsset(ctx, asset.AssetID)
	if err != nil || !reflect.DeepEqual(after.Artifacts, asset.Artifacts) || after.Status != asset.Status || after.HumanApproved != asset.HumanApproved {
		t.Fatal("original asset changed", after, err)
	}
	for p, sha := range parent {
		b, e := os.ReadFile(p)
		if e != nil || hashBytes(b) != sha {
			t.Fatal("parent changed", p, e)
		}
	}
	out := filepath.Join(repo, ".run-data/ass-subtitles-2026-10-07/real-chain")
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	job, err := getTestStore(svc).GetDeliveryVariantJob(ctx, view.VariantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"subtitles.ass", "subtitles.srt", "ass-style.json", "variant-report.json", "variant-receipt.json", "variant-manifest.json"} {
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(job.OutputDir), name))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(out, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	raw, _ = json.MarshalIndent(done, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "result.json"), raw, 0600)
	for _, key := range []string{"ass", "srt", "report"} {
		r, e := http.Get(server.URL + "/api/delivery-variants/" + view.VariantID + "/files/" + key)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Disposition"), "attachment") || r.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("file download", key, r.StatusCode)
		}
		if key == "srt" && !bytes.Equal(b, []byte(srt)) {
			t.Fatal("SRT bytes differ")
		}
	}
	zr, err := http.Get(server.URL + "/api/delivery-variants/" + view.VariantID + "/delivery.zip")
	if err != nil {
		t.Fatal(err)
	}
	zipBytes, _ := io.ReadAll(zr.Body)
	zr.Body.Close()
	if zr.StatusCode != 200 {
		t.Fatal(zr.StatusCode)
	}
	archive, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "delivery.zip"), zipBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "frozen-input.json"), []byte(job.InputJSON), 0600); err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, f := range archive.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(r)
		r.Close()
		entries[f.Name] = b
	}
	if len(entries) != len(done.Artifacts)+1 || !bytes.Equal(entries["subtitles.srt"], []byte(srt)) {
		t.Fatal("incomplete ZIP", len(entries))
	}
	for _, a := range done.Artifacts {
		if hashBytes(entries[a.Path]) != a.SHA256 {
			t.Fatal("ZIP hash", a.Path)
		}
	}
	// Render with the exact requested locally installed font; never download fonts.
	font := "C:/Windows/Fonts/msyh.ttc"
	fontBytes, err := os.ReadFile(font)
	if err != nil {
		t.Fatal("Microsoft YaHei rendering acceptance unavailable", err)
	}
	render := filepath.Join(root, "ass-render")
	_ = os.MkdirAll(filepath.Join(render, "fonts"), 0700)
	_ = os.WriteFile(filepath.Join(render, "fonts/msyh.ttc"), fontBytes, 0600)
	assBytes, _ := os.ReadFile(filepath.Join(out, "subtitles.ass"))
	_ = os.WriteFile(filepath.Join(render, "subtitles.ass"), assBytes, 0600)
	generate(render, "-f", "lavfi", "-i", "color=c=black:size=1280x720:rate=30:duration=1", "-vf", "ass=subtitles.ass:fontsdir=fonts", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", filepath.Join(out, "ass-render.mp4"))
	frame := generate(out, "-i", "ass-render.mp4", "-ss", "0.5", "-frames:v", "1", "-pix_fmt", "gray", "-f", "rawvideo", "pipe:1")
	if len(frame) != 1280*720 {
		t.Fatal("wrong rendered frame size", len(frame))
	}
	brightTop, brightBottom := 0, 0
	minX, maxX, minY, maxY := 1280, 0, 720, 0
	for i, p := range frame {
		if p > 80 {
			x, y := i%1280, i/1280
			minX = min(minX, x)
			maxX = max(maxX, x)
			minY = min(minY, y)
			maxY = max(maxY, y)
			if i/1280 < 360 {
				brightTop++
			} else {
				brightBottom++
			}
		}
	}
	if brightTop != 0 || brightBottom < 50 {
		t.Fatal("ASS text missing or literal override moved it", brightTop, brightBottom)
	}
	if minX < 60 || maxX > 1220 || maxY > 688 || maxY-minY < 70 {
		t.Fatal("long subtitle did not wrap in safe area", minX, maxX, minY, maxY)
	}
	before := generate(out, "-i", "ass-render.mp4", "-ss", "0", "-frames:v", "1", "-pix_fmt", "gray", "-f", "rawvideo", "pipe:1")
	for _, p := range before {
		if p > 80 {
			t.Fatal("subtitle rendered before start")
		}
	}
	generate(out, "-i", "ass-render.mp4", "-ss", "0.5", "-frames:v", "1", "ass-render.png")
	evidence := map[string]any{"mode": "real_go_rest_mcp_worker_ffmpeg_libass", "status": "passed", "subtitle_count": 2, "parent_unchanged": true, "srt_bytes_unchanged": true, "zip_registered_files": len(entries), "font_path": font, "bright_top_pixels": brightTop, "bright_bottom_pixels": brightBottom, "literal_override_stays_bottom": true, "long_text_wrap_safe_area": true, "text_bounds": []int{minX, minY, maxX, maxY}, "human_visual_acceptance": "pending"}
	raw, _ = json.MarshalIndent(evidence, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "acceptance.json"), raw, 0600)
	if os.Getenv("VAC_ASS_BROWSER_CHECK") == "1" {
		browser := exec.CommandContext(ctx, "node", filepath.Join(repo, "scripts/verify-ass-live.mjs"), server.URL, run.RunID)
		browser.Env = append(os.Environ(), "VAC_TEST_OUTPUT="+filepath.Join(out, "browser"))
		if b, e := browser.CombinedOutput(); e != nil {
			t.Fatalf("browser %v %s", e, b)
		} else {
			t.Log(string(b))
		}
	}
}
