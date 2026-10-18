package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

// Opt in under TestWorkspace on a host with FFmpeg and Microsoft YaHei. Models
// and speech still use loopback stubs; rendering, submission and probing are real.
func TestAutoE2EHighWithRealPreview(t *testing.T) {
	if os.Getenv("VAC_AUTO_REAL_PREVIEW") != "1" {
		t.Skip("set VAC_AUTO_REAL_PREVIEW=1 under TestWorkspace to test the installed renderer and font")
	}
	output := os.Getenv("VAC_TEST_OUTPUT")
	if output == "" {
		t.Fatal("managed VAC_TEST_OUTPUT is required")
	}
	e := newE2EEnvWithCanvas(t, "1280x720")
	e.realPreview = true
	e.startAuto("团战", serviceScope())
	if !e.pump(e.settled) || e.auto().State != model.AutoStateCompleted {
		t.Fatalf("not completed: %+v; worker logs: %v", e.auto(), e.workerLogs)
	}
	e.assertDelivered()
	e.assertStubSawOnlyTheDummyToken()
	jobs, err := e.st.ListPreviewJobs(t.Context(), e.runID)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("preview jobs = %+v, err=%v", jobs, err)
	}
	dir := filepath.Join(e.root, filepath.FromSlash(jobs[0].OutputDir))
	media, err := e.svc.MediaProber().ProbeMedia(t.Context(), filepath.Join(dir, "final.mp4"))
	if err != nil || media.Width != 1280 || media.Height != 720 || media.FrameCount <= 0 || media.VideoCodec != "h264" || media.AudioCodec != "aac" {
		t.Fatalf("real media = %+v, err=%v", media, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "preview-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt preview.Receipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Font == nil || receipt.Font.Family != "Microsoft YaHei" || len(receipt.Font.SHA256) != 64 || receipt.FFmpegVersion == "e2e" {
		t.Fatalf("real font/renderer evidence missing: %+v", receipt)
	}
	out := filepath.Join(output, "auto-real-preview")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"final.mp4", "preview-receipt.json", "render-report.json", "preview-manifest.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real preview: %dx%d, %d frames, %.3fs; font %s sha256=%s; evidence %s", media.Width, media.Height, media.FrameCount, media.DurationSeconds, receipt.Font.Family, receipt.Font.SHA256, out)
}
