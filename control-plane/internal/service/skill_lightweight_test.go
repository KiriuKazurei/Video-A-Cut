package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

// Opt-in acceptance uses an already installed model and a local speech fixture.
// It never downloads weights, contacts a provider, or modifies the model directory.
func TestProjectLocalTranscriptionRealRuntime(t *testing.T) {
	modelPath, python, audio := os.Getenv("VAC_ASR_TEST_MODEL"), os.Getenv("VAC_ASR_TEST_PYTHON"), os.Getenv("VAC_ASR_TEST_AUDIO")
	if modelPath == "" || python == "" {
		t.Skip("explicit local ASR model and Python required")
	}
	if audio == "" {
		audio = filepath.Join(t.TempDir(), "local-speech.wav")
		cmd := exec.Command("C:/Program Files/PowerShell/7/pwsh.exe", "-Command", `$ErrorActionPreference='Stop'; $voice=New-Object -ComObject SAPI.SpVoice; $stream=New-Object -ComObject SAPI.SpFileStream; $stream.Open($env:VAC_ASR_SPEECH_OUTPUT,3,$false); try { $voice.AudioOutputStream=$stream; [void]$voice.Speak('This is a local transcription test. All video editing stays on this computer. We are checking speech recognition without any cloud service.'); } finally { $stream.Close() }`)
		cmd.Env = append(os.Environ(), "VAC_ASR_SPEECH_OUTPUT="+audio)
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("explicit local speech fixture unavailable: %v %s", err, raw)
		}
	}
	svc, _, _ := scriptFixture(t)
	ctx := t.Context()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureTTS(python, filepath.Join(root, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureProjectWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseProjectWorkspace)
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(t.TempDir(), "local.avp"), Name: "Offline transcription"})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, audio)
	if err != nil {
		t.Fatal(err)
	}
	profile := builtinProfile(1)
	profile.Transcription = &preparation.TranscriptionProvider{APIFormat: "faster_whisper", ModelPath: modelPath, PythonExecutable: python, Device: "cpu", ComputeType: "int8", Language: "en", CPUThreads: 4}
	probe, err := svc.CheckLocalTranscription(ctx, *profile.Transcription)
	if err != nil || !probe.OK {
		t.Fatalf("local environment: %+v %v", probe, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.CheckLocalTranscription(cancelled, *profile.Transcription); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled local probe: %v", err)
	}
	if _, err = svc.SaveProcessingProfile(ctx, "human:test", "local-asr-profile", 0, profile); err != nil {
		t.Fatal(err)
	}
	visible, err := svc.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if err != nil || len(visible.Media) != 1 {
		t.Fatalf("project audio catalogue: %+v %v", visible, err)
	}
	actor := "agent:local-asr"
	r, err := svc.CreateSkillRun(ctx, actor, skill.RunRequest{ProjectID: view.Document.Project.ID, SkillID: "video-use", Mode: "external_mcp", VisibleSHA: visible.SHA, Goal: "Transcribe locally without cloud credentials", ProfileID: profile.ProfileID, ProfileRevision: 1, IdempotencyKey: "local-asr"})
	if err != nil {
		t.Fatal(err)
	}
	arguments, _ := json.Marshal(map[string]string{"media_id": visible.Media[0].ID})
	r, raw, err := svc.ExecuteSkillTool(ctx, actor, r.ID, skill.ToolRequest{Name: "media.transcribe", Arguments: arguments, ExpectedVersion: r.Version, IdempotencyKey: "transcribe"})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Transcript struct {
			Text, Provider   string
			Words            []struct{ Start, End float64 }
			ModelFingerprint string `json:"model_fingerprint"`
		} `json:"transcript"`
		Artifact skill.Artifact `json:"artifact"`
	}
	if err = json.Unmarshal(raw, &result); err != nil || result.Transcript.Provider != "faster-whisper" || len(result.Transcript.Words) == 0 || result.Transcript.ModelFingerprint != probe.ModelFingerprint {
		t.Fatalf("actual word transcription: %s %v", raw, err)
	}
	for _, word := range result.Transcript.Words {
		if word.Start < visible.Media[0].In || word.End < word.Start || word.End > visible.Media[0].Out+.1 {
			t.Fatalf("source timestamps out of range: %+v", word)
		}
	}
	t.Logf("local ASR: %s; %d source-timed words; model %s", result.Transcript.Text, len(result.Transcript.Words), probe.ModelFingerprint)
	_, again, err := svc.ExecuteSkillTool(ctx, actor, r.ID, skill.ToolRequest{Name: "media.transcribe", Arguments: arguments, ExpectedVersion: r.Version, IdempotencyKey: "transcribe"})
	if err != nil || string(again) != string(raw) {
		t.Fatalf("idempotent transcript replay: %v", err)
	}
	_, err = svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(t.TempDir(), "other.avp"), Name: "Other", Decision: "discard", StopTasks: true, ExpectedSession: view.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.SkillArtifactPath(ctx, actor, r.ID, result.Artifact.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("old transcript crossed project boundary: %v", err)
	}
}

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
