package service_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

// Real FFmpeg + local transcript/model fixtures exercise both halves around the
// human gate. This is not a claim about a real provider's editing quality.
func TestSkillLightweightBuiltinFromTranscriptToMP4(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	svc, _, media := scriptFixture(t)
	ctx := t.Context()
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30:duration=3", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", media[0].Path)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("media: %v %s", err, raw)
	}
	root, _ := filepath.Abs("../../..")
	if err := svc.ConfigureTTS("python", filepath.Join(root, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	transcriptions := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 4<<20))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(request.URL.Path, "/speech-to-text") {
			transcriptions++
			_ = json.NewEncoder(w).Encode(map[string]any{"words": []any{map[string]any{"text": "Hello", "start": 0., "end": .5, "type": "word"}}})
			return
		}
		var body struct {
			Messages []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(bytes.NewReader(raw)).Decode(&body) != nil || len(body.Messages) == 0 {
			w.WriteHeader(400)
			return
		}
		prompt := body.Messages[0].Content[0].Text
		at := strings.LastIndex(prompt, "\n")
		var data struct {
			Run     skill.Run `json:"run"`
			History []struct {
				Result json.RawMessage `json:"result"`
			} `json:"history"`
		}
		if json.Unmarshal([]byte(prompt[at+1:]), &data) != nil {
			w.WriteHeader(400)
			return
		}
		tool := ""
		args := map[string]any{}
		hasTranscript, hasMP4 := false, false
		for _, a := range data.Run.Artifacts {
			hasTranscript = hasTranscript || a.Kind == "transcript"
			hasMP4 = hasMP4 || a.Kind == "mp4"
		}
		switch {
		case data.Run.Plan == nil && !hasTranscript:
			tool = "media.transcribe"
			args["media_id"] = media[0].ID
		case data.Run.Plan == nil:
			tool = "scripts.propose"
			args = map[string]any{"body": "Keep the spoken moment", "segments": []any{map[string]any{"media_id": media[0].ID, "start": 0, "end": 1, "narration": "Hello"}}, "subtitles": false}
		case !hasMP4:
			tool = "renders.create"
		case data.Run.Inspection == nil:
			tool = "renders.inspect"
		case data.Run.Inspection.Verdict != "passed":
			viewed := map[string]bool{}
			for _, h := range data.History {
				var row struct {
					Artifact skill.Artifact `json:"artifact"`
				}
				_ = json.Unmarshal(h.Result, &row)
				viewed[row.Artifact.ID] = true
			}
			for _, frame := range data.Run.Inspection.FrameIDs {
				if !viewed[frame] {
					tool = "artifacts.get"
					args["id"] = frame
					break
				}
			}
			if tool == "" {
				tool = "renders.review"
				args = map[string]any{"plan_version": data.Run.Plan.Version, "frame_ids": data.Run.Inspection.FrameIDs, "verdict": "passed", "issues": []string{}}
			}
		default:
			tool = "run.finish"
		}
		content, _ := json.Marshal(map[string]any{"tool": tool, "arguments": args})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}}})
	}))
	defer provider.Close()
	t.Setenv("VAC_LIGHTWEIGHT_TOKEN", "local-only-fixture")
	profile := builtinProfile(1)
	profile.ContentMode = "configured"
	profile.Vision = preparation.Provider{Adapter: "vision", Endpoint: provider.URL + "/v1", Model: "fixture", TokenEnv: "VAC_LIGHTWEIGHT_TOKEN", APIFormat: "openai"}
	profile.Narration = profile.Vision
	profile.Narration.Adapter = "narration"
	profile.Transcription = &preparation.TranscriptionProvider{APIFormat: "elevenlabs", Endpoint: provider.URL + "/v1", Model: "scribe_v1", TokenEnv: "VAC_LIGHTWEIGHT_TOKEN"}
	if _, err := svc.SaveProcessingProfile(ctx, "human:test", "asr-profile", 0, profile); err != nil {
		t.Fatal(err)
	}
	p, err := svc.SaveProject(ctx, "human:test", "", service.ProjectRequest{Name: "Lightweight", Flow: "lightweight", Assets: []string{"clip"}, Visible: []string{"clip"}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectVisibleSet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ConfirmProjectVisible(ctx, "human:test", p.ID, view.SHA, p.Version); err != nil {
		t.Fatal(err)
	}
	r, err := svc.CreateSkillRun(ctx, "human:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "builtin_api", VisibleSHA: view.SHA, Goal: "Make a short video", ProfileID: profile.ProfileID, ProfileRevision: 1, AcceptTranscription: true, IdempotencyKey: "lightweight"})
	if err != nil {
		t.Fatal(err)
	}
	r, err = svc.RunBuiltinAgent(ctx, "human:test", r.ID, r.Version)
	if err != nil || r.State != "waiting_human" {
		t.Fatalf("draft %+v %v", r, err)
	}
	r, err = svc.UpdateSkillRun(ctx, "human:test", r.ID, "finalize", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	r, err = svc.RunBuiltinAgent(ctx, "human:test", r.ID, r.Version)
	if err != nil || r.State != "succeeded" {
		t.Fatalf("delivery %+v %v", r, err)
	}
	if transcriptions != 1 || r.Inspection == nil || r.Inspection.Verdict != "passed" {
		t.Fatalf("cache or self-check failed: %d %+v", transcriptions, r.Inspection)
	}
}
