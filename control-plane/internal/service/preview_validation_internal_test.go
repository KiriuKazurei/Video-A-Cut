package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

type timelineSourceProber struct{ sameCanvas bool }

func (p timelineSourceProber) ProbeMedia(_ context.Context, path string) (preview.Media, error) {
	if p.sameCanvas || filepath.Base(path) == "earlier.mp4" {
		return preview.Media{Width: 320, Height: 180}, nil
	}
	return preview.Media{Width: 1280, Height: 720}, nil
}

func TestPreviewFrozenTimelineCanvasAndOverlap(t *testing.T) {
	for _, tc := range []struct {
		name        string
		secondStart float64
		frames      int64
		valid       bool
		fontSHA     string
	}{
		{"unsorted_video_uses_earliest_clip_canvas", 1, 60, true, ""},
		{"overlap_cannot_be_hidden_by_a_passed_report", 0.5, 45, false, ""},
		{"font_hash_must_be_hex", 1, 60, false, strings.Repeat("z", 64)},
		{"font_hash_must_be_lowercase", 1, 60, false, strings.Repeat("A", 64)},
		{"valid_font_hash", 1, 60, true, strings.Repeat("a", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			pkg := filepath.Join(root, "package")
			if err := os.Mkdir(pkg, 0700); err != nil {
				t.Fatal(err)
			}
			edl := map[string]any{"timeline": map[string]int{"fps": 30}, "video": []map[string]any{
				{"src": "later.mp4", "in": 0, "out": 1, "timeline_in": tc.secondStart},
				{"src": "earlier.mp4", "in": 0, "out": 1, "timeline_in": 0},
			}}
			data, _ := json.Marshal(edl)
			for name, content := range map[string][]byte{"edl.json": data, "earlier.mp4": {}, "later.mp4": {}} {
				if err := os.WriteFile(filepath.Join(pkg, name), content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			svc := &Service{}
			if err := svc.ConfigureDeliveryRoot(root); err != nil {
				t.Fatal(err)
			}
			svc.SetMediaProber(timelineSourceProber{sameCanvas: !tc.valid})
			input := preview.Input{Files: []preview.InputFile{{Kind: "source_video", Path: "earlier.mp4"}, {Kind: "source_video", Path: "later.mp4"}}}
			media := preview.Media{Width: 320, Height: 180, FPS: 30, FrameCount: tc.frames, DurationSeconds: float64(tc.frames) / 30, VideoCodec: "h264", AudioCodec: "aac", SampleRate: 48000, Channels: 2}
			report := preview.Report{TargetFrames: tc.frames, TargetSamples: tc.frames * 1600, Media: media}
			for _, code := range []string{"source_fingerprint", "timeline_frames", "audio_alignment", "subtitles", "media_streams"} {
				report.Checks = append(report.Checks, preview.Check{Code: code, Status: "passed"})
			}
			receipt := preview.Receipt{RendererVersion: "test", FFmpegVersion: "test"}
			if tc.fontSHA != "" {
				input.Files = append(input.Files, preview.InputFile{Kind: "srt", Path: "subtitles.srt"})
				receipt.Font = &preview.Font{Family: "Microsoft YaHei", SHA256: tc.fontSHA}
			}
			err := svc.validatePreviewMedia(context.Background(), input, report, receipt, media, pkg)
			if tc.valid && err != nil {
				t.Fatalf("valid timeline rejected: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("invalid frozen timeline or receipt was accepted")
			}
		})
	}
}
