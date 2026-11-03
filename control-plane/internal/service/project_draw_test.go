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
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func TestNativeAgentCorrectsTimelineArgumentsWithoutHumanResume(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Name: "Autonomous", Path: filepath.Join(root, "autonomous.avp")})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := svc.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateSkillRun(ctx, "agent:draw", skill.RunRequest{ProjectID: view.Document.Project.ID, SequenceID: view.Document.Sequences[0].ID, SkillID: "vac-draw-remotion", Mode: "external_mcp", Strategy: "draw", VisibleSHA: visible.SHA, Goal: "correct a typo", IdempotencyKey: "autonomous"})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = svc.ExecuteSkillTool(ctx, "agent:draw", run.ID, skill.ToolRequest{Name: "timeline.edit", ExpectedVersion: run.Version, IdempotencyKey: "bad-op", Arguments: json.RawMessage(`{"command":{"operations":[{"op":"track.add"}]}}`)})
	if !errors.Is(err, model.ErrArgument) || run.State != "queued" || run.Reason != "invalid_tool_arguments" {
		t.Fatalf("argument repair required human: %+v %v", run, err)
	}
	run, _, err = svc.ExecuteSkillTool(ctx, "agent:draw", run.ID, skill.ToolRequest{Name: "timeline.read", ExpectedVersion: run.Version, IdempotencyKey: "corrected", Arguments: json.RawMessage(`{}`)})
	if err != nil || run.State != "queued" {
		t.Fatalf("owner could not continue: %+v %v", run, err)
	}
	current, err := svc.ProjectWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	doc := project.DrawDocument{SchemaVersion: 1, Canvas: project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}}, DurationFrames: 15, Sources: []project.DrawSource{{ID: "react", Engine: "remotion", Source: "export default ()=>null"}}, Nodes: []project.DrawNode{{ID: "scene", Kind: "source", SourceID: "react", Duration: 15}}}
	save := service.DrawSaveRequest{Session: current.ID, ExpectedRevision: current.Revision, IdempotencyKey: "unified", Composition: project.DrawComposition{ID: "unified", Name: "Unified Remotion", Document: &doc}}
	args, _ := json.Marshal(save)
	run, _, err = svc.ExecuteSkillTool(ctx, "agent:draw", run.ID, skill.ToolRequest{Name: "draw.save", ExpectedVersion: run.Version, IdempotencyKey: "unified", Arguments: args})
	if err != nil || run.State != "queued" {
		t.Fatalf("valid unified source mistaken for missing authorization: %+v %v", run, err)
	}
}

func TestNativeAgentSharesDesktopPreview(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	sourceRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureTTS("python", filepath.Join(sourceRoot, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Name: "Shared preview", Path: filepath.Join(root, "shared.avp")})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, filepath.Join(sourceRoot, "workers", "node", "timeline-cli", "fixtures", "media", "clip_01.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	seq := view.Document.Sequences[0]
	end := project.Time{Value: 1, Scale: 2}
	timeline, err := svc.TimelineCommand(ctx, "agent:draw", view.Document.Project.ID, seq.ID, project.Command{Session: view.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "insert", Operations: []project.Operation{{Type: "clip.insert", TrackID: seq.Tracks[0].ID, ResourceID: view.Document.Resources[0].ID, SourceOut: &end}}})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		preview service.SequencePreview
		err     error
	}
	start := make(chan struct{})
	done := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			p, e := svc.CreateSequencePreview(ctx, view.Document.Project.ID, seq.ID, service.SequencePreviewRequest{Session: view.ID, ExpectedRevision: timeline.Sequence.Revision, ContentVersion: timeline.ContentVersion})
			done <- result{p, e}
		}()
	}
	close(start)
	a, b := <-done, <-done
	if a.err != nil || b.err != nil || a.preview.SHA256 == "" || a.preview.SHA256 != b.preview.SHA256 {
		t.Fatalf("desktop/Agent preview did not share output: %+v %+v", a, b)
	}
}

func TestNativeAgentPlanContinuesWithoutHumanFinalization(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Name: "Autonomous plan", Path: filepath.Join(root, "plan.avp")})
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs("../../../workers/node/timeline-cli/fixtures/media/clip_01.mp4")
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, fixture)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := svc.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if err != nil || len(visible.Media) == 0 {
		t.Fatalf("fixture media: %+v %v", visible, err)
	}
	run, err := svc.CreateSkillRun(ctx, "agent:cut", skill.RunRequest{ProjectID: view.Document.Project.ID, SequenceID: view.Document.Sequences[0].ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "editor", VisibleSHA: visible.SHA, Goal: "cut source", IdempotencyKey: "cut"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(skill.Plan{Body: "Opening", Segments: []skill.Segment{{MediaID: visible.Media[0].ID, Start: 0, End: .5}}})
	run, _, err = svc.ExecuteSkillTool(ctx, "agent:cut", run.ID, skill.ToolRequest{Name: "scripts.propose", ExpectedVersion: run.Version, IdempotencyKey: "plan", Arguments: body})
	if err != nil || run.State != "queued" || run.Plan == nil || run.Plan.FinalizedBy != "agent:cut" || run.Plan.FinalizedAt == nil {
		t.Fatalf("native plan blocked Agent: %+v %v", run, err)
	}
}

func TestDrawCreateRefreshesLegacyNativeMediaFingerprint(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Name: "Restored native", Path: filepath.Join(root, "restored.avp")})
	if err != nil {
		t.Fatal(err)
	}
	value, _ := storesByService.Load(svc)
	st := value.(*store.Store)
	if _, err = st.DB().ExecContext(ctx, "UPDATE vac_projects SET body=json_set(body,'$.confirmed_sha','legacy-fingerprint') WHERE id=?", view.Document.Project.ID); err != nil {
		t.Fatal(err)
	}
	visible, err := svc.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if err != nil || !visible.Confirmed {
		t.Fatalf("native visible set: %+v %v", visible, err)
	}
	req := skill.RunRequest{ProjectID: view.Document.Project.ID, SequenceID: view.Document.Sequences[0].ID, SkillID: "vac-draw-remotion", Mode: "external_mcp", Strategy: "draw", VisibleSHA: visible.SHA, Goal: "draw restored project", IdempotencyKey: "restored-native"}
	run, err := svc.CreateSkillRun(ctx, "agent:draw", req)
	if err != nil {
		t.Fatalf("native read model is current but creation still compares obsolete fingerprint: %v", err)
	}
	if run.VisibleSHA != visible.SHA {
		t.Fatal("run did not bind the current fingerprint")
	}
	req.VisibleSHA = "legacy-fingerprint"
	req.IdempotencyKey = "reject-old"
	if _, err = svc.CreateSkillRun(ctx, "agent:draw", req); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("old caller fingerprint must still be rejected: %v", err)
	}
}

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
	if !errors.Is(e, model.ErrArgument) {
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
			decision = map[string]any{"tool": "timeline.edit", "arguments": map[string]any{"command": map[string]any{"operations": []any{map[string]any{"op": "track.add"}}}}}
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

func TestDrawReservedTrackImport(t *testing.T) {
	for _, mode := range []string{"snapshot", "follow", "rendered"} {
		t.Run(mode, func(t *testing.T) {
			svc, root := avpWorkspaceFixture(t)
			ctx := t.Context()
			view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "reserved.avp"), Name: "Reserved"})
			if e != nil {
				t.Fatal(e)
			}
			doc := project.DrawDocument{SchemaVersion: 1, Canvas: project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}}, DurationFrames: 30, Preset: "auto", Sources: []project.DrawSource{}, Nodes: []project.DrawNode{{ID: "text", Kind: "text", Text: "Editable source", Duration: 30}}}
			view, e = svc.SaveDrawComposition(ctx, view.Document.Project.ID, service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "source", Composition: project.DrawComposition{ID: "scene", Name: "Scene", Document: &doc}})
			if e != nil {
				t.Fatal(e)
			}
			pid := view.Document.Project.ID
			seq := view.Document.Sequences[0]
			trackID := seq.Tracks[0].ID
			c := view.Document.DrawCompositions[0]
			before, e := svc.Timeline(ctx, pid, seq.ID)
			if e != nil {
				t.Fatal(e)
			}
			locked := true
			held, e := svc.TimelineCommand(ctx, "agent:render", pid, seq.ID, project.Command{Session: view.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "reserve", Operations: []project.Operation{{Type: "track.update", TrackID: trackID, Locked: &locked}}})
			if e != nil {
				t.Fatal(e)
			}

			if mode == "rendered" {
				repo, e := filepath.Abs("../../..")
				if e != nil {
					t.Fatal(e)
				}
				if e = svc.ConfigureTTS("python", filepath.Join(repo, "workers", "python")); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(svc.CloseTTS)
				movie, e := svc.RenderDrawComposition(ctx, pid, c.ID, service.DrawActionRequest{Session: view.ID, ContentVersion: c.ContentVersion})
				if e != nil {
					t.Fatal(e)
				}
				if movie.Frames != 30 || movie.Size == 0 {
					t.Fatal("actual drawing movie missing")
				}
			}
			if held.ContentVersion != before.ContentVersion {
				t.Fatal("lock-only update invalidated rendered content")
			}
			raw, _ := json.Marshal(map[string]any{"session": view.ID, "content_version": c.ContentVersion, "expected_revision": held.Sequence.Revision, "idempotency_key": "attach", "sequence_id": seq.ID, "source_mode": mode, "target_track_id": trackID, "at": 30})
			var req service.DrawImportRequest
			if e = json.Unmarshal(raw, &req); e != nil {
				t.Fatal(e)
			}
			attached, e := svc.ImportDrawComposition(ctx, "agent:render", pid, c.ID, req)
			if e != nil {
				t.Fatal(e)
			}
			if len(attached.Sequence.Tracks) != len(seq.Tracks) || !attached.Sequence.Tracks[0].Locked || len(attached.Sequence.Tracks[0].Clips) != 1 || attached.Sequence.Tracks[0].Clips[0].At != 30 {
				t.Fatal("source was not inserted into the reserved video track while still locked")
			}
			replay, e := svc.ImportDrawComposition(ctx, "agent:render", pid, c.ID, req)
			if e != nil || len(replay.Sequence.Tracks[0].Clips) != 1 {
				t.Fatalf("import replay duplicated insert: %v", e)
			}
			locked = false
			released, e := svc.TimelineCommand(ctx, "agent:render", pid, seq.ID, project.Command{Session: view.ID, ExpectedRevision: attached.Sequence.Revision, IdempotencyKey: "release", Operations: []project.Operation{{Type: "track.update", TrackID: trackID, Locked: &locked}}})
			if e != nil {
				t.Fatal(e)
			}
			if released.Sequence.Tracks[0].Locked {
				t.Fatal("reservation did not release")
			}
		})
	}
}

func TestDrawingAgentReservationCleanup(t *testing.T) {
	for _, action := range []string{"cancel", "tool_failure"} {
		t.Run(action, func(t *testing.T) {
			svc, root := avpWorkspaceFixture(t)
			ctx := t.Context()
			if e := svc.ConfigureSkillHarness(ctx); e != nil {
				t.Fatal(e)
			}
			v, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "cleanup.avp"), Name: "Cleanup"})
			if e != nil {
				t.Fatal(e)
			}
			pid, seq := v.Document.Project.ID, v.Document.Sequences[0]
			visible, e := svc.ProjectVisibleSet(ctx, pid)
			if e != nil {
				t.Fatal(e)
			}
			run, e := svc.CreateSkillRun(ctx, "agent:worker", skill.RunRequest{ProjectID: pid, SequenceID: seq.ID, SkillID: "vac-draw-remotion", Mode: "external_mcp", Strategy: "draw", VisibleSHA: visible.SHA, Goal: "render insertion", IdempotencyKey: "cleanup"})
			if e != nil {
				t.Fatal(e)
			}
			lock := true
			args, _ := json.Marshal(map[string]any{"sequence_id": seq.ID, "command": project.Command{Session: v.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "lock", Operations: []project.Operation{{Type: "track.update", TrackID: seq.Tracks[0].ID, Locked: &lock}}}})
			run, _, e = svc.ExecuteSkillTool(ctx, "agent:worker", run.ID, skill.ToolRequest{Name: "timeline.edit", ExpectedVersion: run.Version, IdempotencyKey: "lock", Arguments: args})
			if e != nil {
				t.Fatal(e)
			}
			held, e := svc.Timeline(ctx, pid, seq.ID)
			if e != nil || !held.Sequence.Tracks[0].Locked {
				t.Fatal("Agent reservation missing")
			}

			_, _, stale := svc.ExecuteSkillTool(ctx, "agent:worker", run.ID, skill.ToolRequest{Name: "timeline.read", ExpectedVersion: run.Version - 1, IdempotencyKey: "stale-request", Arguments: json.RawMessage(`{}`)})
			if !errors.Is(stale, model.ErrConflict) {
				t.Fatal("expected stale request conflict")
			}
			stillHeld, _ := svc.Timeline(ctx, pid, seq.ID)
			if !stillHeld.Sequence.Tracks[0].Locked {
				t.Fatal("a stale request released an active reservation")
			}
			// A manual lock on another track must survive either exit path.
			_, e = svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: v.ID, ExpectedRevision: held.Sequence.Revision, IdempotencyKey: "manual", Operations: []project.Operation{{Type: "track.update", TrackID: seq.Tracks[1].ID, Locked: &lock}}})
			if e != nil {
				t.Fatal(e)
			}
			if action == "cancel" {
				_, e = svc.UpdateSkillRun(ctx, "agent:worker", run.ID, "cancel", run.Version)
				if e != nil {
					t.Fatal(e)
				}
			} else {
				_, _, e = svc.ExecuteSkillTool(ctx, "agent:worker", run.ID, skill.ToolRequest{Name: "draw.check", ExpectedVersion: run.Version, IdempotencyKey: "missing", Arguments: json.RawMessage(`{"composition_id":"missing","session":"missing","content_version":"missing"}`)})
				if e == nil {
					t.Fatal("expected failed tool")
				}
			}
			after, e := svc.Timeline(ctx, pid, seq.ID)
			if e != nil {
				t.Fatal(e)
			}
			if after.Sequence.Tracks[0].Locked || !after.Sequence.Tracks[1].Locked {
				t.Fatal("Agent reservation stuck or manual lock released")
			}
		})
	}
}
