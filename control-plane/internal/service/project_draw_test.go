package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

// The model endpoint is a local fixture: it proves streamed progress reaches
// the service while generation is still in flight without storing model text.
func TestDrawBuiltinStreamingProgress(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	sourceRoot, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ConfigureTTS("python", filepath.Join(sourceRoot, "workers", "python")); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(svc.CloseTTS)
	if e = svc.ConfigureSkillHarness(ctx); e != nil {
		t.Fatal(e)
	}
	view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "stream.avp"), Name: "Stream"})
	if e != nil {
		t.Fatal(e)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) > 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"wait":"done"}`}}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(content string, finish any) {
			payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}, "finish_reason": finish}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			w.(http.Flusher).Flush()
		}
		send(`{"tool":"draw.list",`, nil)
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		send(`"arguments":{}}`, nil)
		send("", "stop")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	t.Setenv("VAC_SKILL_TEST_TOKEN", "fixture-only")
	profile := builtinProfile(1)
	profile.ContentMode = "configured"
	profile.Vision = preparation.Provider{Adapter: "vision", Endpoint: provider.URL + "/v1", Model: "step-5-preview", TokenEnv: "VAC_SKILL_TEST_TOKEN", APIFormat: "openai", ReasoningEffort: "high"}
	profile.Narration = profile.Vision
	profile.Narration.Adapter = "narration"
	if _, e = svc.SaveProcessingProfile(ctx, "human:test", "skill-profile", 0, profile); e != nil {
		t.Fatal(e)
	}
	visible, e := svc.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if e != nil {
		t.Fatal(e)
	}
	run, e := svc.CreateSkillRun(ctx, "human:test", skill.RunRequest{ProjectID: view.Document.Project.ID, SkillID: "vac-draw-pipeline", Mode: "builtin_api", VisibleSHA: visible.SHA, Goal: "draw", ProfileID: profile.ProfileID, ProfileRevision: 1, AcceptExternal: true, IdempotencyKey: "stream"})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, err := svc.RunBuiltinAgent(ctx, "human:test", run.ID, run.Version); done <- err }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("model request did not start")
	}
	deadline := time.Now().Add(5 * time.Second)
	var current skill.Run
	for time.Now().Before(deadline) {
		current, e = svc.GetSkillRun(ctx, "human:test", run.ID)
		if e == nil && current.Progress != nil && current.Progress.OutputChars > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if current.State != "running" || current.Progress == nil || current.Progress.Phase != "generating" || current.Progress.OutputChars == 0 {
		t.Fatalf("missing live progress: %+v %v", current, e)
	}
	close(release)
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("agent did not finish")
	}
}

func TestDrawProgressCannotCrossProjectSwitch(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	if e := svc.ConfigureSkillHarness(ctx); e != nil {
		t.Fatal(e)
	}
	a, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "first.avp"), Name: "First"})
	if e != nil {
		t.Fatal(e)
	}
	visible, e := svc.ProjectVisibleSet(ctx, a.Document.Project.ID)
	if e != nil {
		t.Fatal(e)
	}
	r, e := svc.CreateSkillRun(ctx, "agent:draw", skill.RunRequest{ProjectID: a.Document.Project.ID, SequenceID: a.Document.Sequences[0].ID, SkillID: "vac-draw-pipeline", Mode: "external_mcp", AgentRole: "editor", VisibleSHA: visible.SHA, Goal: "draw", IdempotencyKey: "first"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "second.avp"), Name: "Second", Decision: "discard", StopTasks: true, ExpectedSession: a.ID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ReportSkillProgress(ctx, "agent:draw", r.ID, r.Version, "generating", 10); !errors.Is(e, model.ErrNotFound) {
		t.Fatalf("old project progress accessible after switch: %v", e)
	}
}

func TestDrawSourceTransactions(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	if e := svc.ConfigureSkillHarness(ctx); e != nil {
		t.Fatal(e)
	}
	view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "draw.avp"), Name: "Source transactions"})
	if e != nil {
		t.Fatal(e)
	}
	pid := view.Document.Project.ID
	c := project.DrawComposition{ID: "source", Name: "Title", Engine: "remotion", Source: "export default ()=>null", Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}, DurationFrames: 30}
	req := service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "new-source", Composition: c}
	first, e := svc.SaveDrawComposition(ctx, pid, req)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := svc.SaveDrawComposition(ctx, pid, req)
	if e != nil || replay.Revision != first.Revision {
		t.Fatalf("duplicate changed source: %v", e)
	}
	changed := req
	changed.Composition.Source += " "
	if _, e = svc.SaveDrawComposition(ctx, pid, changed); !errors.Is(e, model.ErrConflict) {
		t.Fatalf("changed replay accepted: %v", e)
	}
	c = first.Document.DrawCompositions[0]
	renamed := c
	renamed.Name = "New label"
	req = service.DrawSaveRequest{Session: first.ID, ExpectedRevision: first.Revision, IdempotencyKey: "rename", Composition: renamed}
	next, e := svc.SaveDrawComposition(ctx, pid, req)
	if e != nil {
		t.Fatal(e)
	}
	saved := next.Document.DrawCompositions[0]
	if saved.ContentVersion != c.ContentVersion || saved.Revision != c.Revision+1 {
		t.Fatal("rename invalidated pixels or lost revision")
	}
	req.ExpectedRevision = next.Revision
	req.IdempotencyKey = "stale-source"
	req.Composition.Source += " "
	if _, e = svc.SaveDrawComposition(ctx, pid, req); !errors.Is(e, model.ErrConflict) {
		t.Fatalf("stale composition accepted: %v", e)
	}
	req.Composition = saved
	req.Composition.Source += " "
	req.IdempotencyKey = "source-update"
	next, e = svc.SaveDrawComposition(ctx, pid, req)
	if e != nil {
		t.Fatal(e)
	}
	if next.Document.DrawCompositions[0].ContentVersion == c.ContentVersion {
		t.Fatal("source edit reused old pixels")
	}
	visible, e := svc.ProjectVisibleSet(ctx, pid)
	if e != nil {
		t.Fatal(e)
	}
	base := skill.RunRequest{ProjectID: pid, SequenceID: view.Document.Sequences[0].ID, Flow: "custom", SkillID: "vac-draw-remotion", Mode: "external_mcp", AgentRole: "editor", VisibleSHA: visible.SHA, Goal: "Create graphics without imported media", IdempotencyKey: "draw"}
	run, e := svc.CreateSkillRun(ctx, "agent:draw", base)
	if e != nil || run.Strategy != "draw" {
		t.Fatalf("empty draw run: %v", e)
	}
	base.SkillID = "video-use"
	base.Flow = "lightweight"
	base.Strategy = "edit"
	base.IdempotencyKey = "edit"
	if _, e = svc.CreateSkillRun(ctx, "agent:draw", base); !errors.Is(e, model.ErrForbidden) {
		t.Fatalf("empty source edit accepted: %v", e)
	}
	// Engine-specific strategies cannot write a composition for another engine.
	current, _ := svc.ProjectWorkspace(ctx)
	other := c
	other.ID = "wrong-engine"
	other.Engine = "hyperframes"
	other.Revision = 0
	other.ContentVersion = ""
	args, _ := json.Marshal(service.DrawSaveRequest{Session: current.ID, ExpectedRevision: current.Revision, IdempotencyKey: "wrong-engine", Composition: other})
	_, _, e = svc.ExecuteSkillTool(ctx, "agent:draw", run.ID, skill.ToolRequest{Name: "draw.save", Arguments: args, ExpectedVersion: run.Version, IdempotencyKey: "wrong-engine"})
	if !errors.Is(e, model.ErrForbidden) {
		t.Fatalf("engine restriction: %v", e)
	}
	list, _ := svc.DrawCompositions(pid)
	if len(list) != 1 {
		t.Fatal("rejected tool mutated project")
	}
}

// The model response is a local fixture. This validates the builtin transport
// and revision handling, not external model quality.
func TestDrawBuiltinSourceCAS(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	sourceRoot, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ConfigureTTS("python", filepath.Join(sourceRoot, "workers", "python")); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(svc.CloseTTS)
	if e = svc.ConfigureSkillHarness(ctx); e != nil {
		t.Fatal(e)
	}
	view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "builtin.avp"), Name: "Builtin draw"})
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	var sawRepairFeedback atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		decision := map[string]any{"wait": "Source saved; fixture ends before rendering"}
		n := calls.Add(1)
		if n == 2 && strings.Contains(string(body), "unknown field") {
			sawRepairFeedback.Store(true)
		}
		if n == 1 {
			decision = map[string]any{"tool": "draw.save", "arguments": map[string]any{"session": view.ID, "expected_revision": 0, "idempotency_key": "invalid", "composition": map[string]any{"document": map[string]any{"images": []any{}}}}}
		}
		if n == 2 {
			c := project.DrawComposition{ID: "builtin-source", Name: "Builtin title", Engine: "remotion", Source: "export default ()=>null", Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}, DurationFrames: 30}
			decision = map[string]any{"tool": "draw.save", "arguments": service.DrawSaveRequest{Session: view.ID, ExpectedRevision: 0, IdempotencyKey: "source", Composition: c}}
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
	if _, e = svc.SaveProcessingProfile(ctx, "human:test", "skill-profile", 0, profile); e != nil {
		t.Fatal(e)
	}
	visible, e := svc.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if e != nil {
		t.Fatal(e)
	}
	run, e := svc.CreateSkillRun(ctx, "human:test", skill.RunRequest{ProjectID: view.Document.Project.ID, SkillID: "vac-draw-remotion", Mode: "builtin_api", VisibleSHA: visible.SHA, Goal: "Author an inline title", ProfileID: profile.ProfileID, ProfileRevision: 1, AcceptExternal: true, IdempotencyKey: "builtin"})
	if e != nil {
		t.Fatal(e)
	}
	run, e = svc.RunBuiltinAgent(ctx, "human:test", run.ID, run.Version)
	if e != nil {
		latest, _ := svc.GetSkillRun(ctx, "human:test", run.ID)
		steps, _ := svc.SkillRunSteps(ctx, "human:test", run.ID)
		t.Fatalf("builtin draw failed: %v, state=%s reason=%s version=%d calls=%d steps=%+v", e, latest.State, latest.Reason, latest.Version, calls.Load(), steps)
	}
	list, e := svc.DrawCompositions(view.Document.Project.ID)
	if e != nil || len(list) != 1 || list[0].ID != "builtin-source" || calls.Load() != 3 || !sawRepairFeedback.Load() || run.State != "waiting_human" {
		t.Fatalf("builtin source flow: %v %v %+v", list, e, run)
	}
}
