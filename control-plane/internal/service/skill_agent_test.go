package service_test

import (
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestSkillBuiltinAgentUsesProviderAndStopsAtHumanGate(t *testing.T) {
	svc, p, v := projectFixture(t)
	ctx := t.Context()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureTTS("python", filepath.Join(root, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the prompt before replying, as real model servers do. An early
		// response can reset a Python upload on Windows for larger Skill prompts.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := calls.Add(1)
		decision := map[string]any{"tool": "media.list", "arguments": map[string]any{}}
		if n > 1 {
			decision = map[string]any{"tool": "scripts.propose", "arguments": map[string]any{"body": "Bound source", "segments": []any{map[string]any{"media_id": v.Media[0].ID, "start": 0, "end": 1, "narration": "Generated draft"}}, "subtitles": false}}
		}
		content, _ := json.Marshal(decision)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}}})
	}))
	defer provider.Close()
	t.Setenv("VAC_SKILL_TEST_TOKEN", "local-fixture-key")
	profile := builtinProfile(1)
	profile.ContentMode = "configured"
	profile.Vision = preparation.Provider{Adapter: "vision", Endpoint: provider.URL + "/v1", Model: "fixture", TokenEnv: "VAC_SKILL_TEST_TOKEN", APIFormat: "openai"}
	profile.Narration = profile.Vision
	profile.Narration.Adapter = "narration"
	if _, err = svc.SaveProcessingProfile(ctx, "human:test", "skill-profile", 0, profile); err != nil {
		t.Fatal(err)
	}
	r, err := svc.CreateSkillRun(ctx, "human:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "builtin_api", VisibleSHA: v.SHA, Goal: "Generate draft", ProfileID: profile.ProfileID, ProfileRevision: 1, AcceptExternal: true, IdempotencyKey: "builtin"})
	if err != nil {
		t.Fatal(err)
	}
	r, err = svc.RunBuiltinAgent(ctx, "human:test", r.ID, r.Version)
	if err != nil {
		t.Fatalf("%v (provider calls=%d)", err, calls.Load())
	}
	if calls.Load() != 2 || r.State != "waiting_human" || r.Reason != "plan_confirmation" || r.Plan == nil || r.Plan.FinalizedBy != "" {
		t.Fatalf("unsafe Agent outcome: %+v calls=%d", r, calls.Load())
	}
	steps, err := svc.SkillRunSteps(ctx, "human:test", r.ID)
	if err != nil || len(steps) != 4 {
		t.Fatalf("model/tool audit missing: %d %v", len(steps), err)
	}
}
