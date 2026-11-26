package service_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

func TestSkillBudgetFollowsRouteAndSkipsReads(t *testing.T) {
	ctx := t.Context()
	svc, p, v := projectFixture(t)
	edit, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "edit budget", IdempotencyKey: "budget-edit"})
	if err != nil {
		t.Fatal(err)
	}
	if edit.Budget.MaxSteps != 48 || edit.Budget.MaxSeconds != 1200 || edit.Budget.MaxFrames != 36 || edit.Remaining.Steps != 48 || edit.Remaining.Frames != 36 {
		t.Fatalf("lightweight budget: %+v remaining %+v", edit.Budget, edit.Remaining)
	}
	mixed, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, Flow: "visual_heavy", SkillID: "vac-visual-summary", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "mixed budget", IdempotencyKey: "budget-mixed"})
	if err != nil {
		t.Fatal(err)
	}
	if mixed.Budget.MaxSteps != 96 || mixed.Budget.MaxSeconds != 2400 || mixed.Budget.MaxFrames != 64 {
		t.Fatalf("mixed budget: %+v", mixed.Budget)
	}
	if _, err = svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "over", IdempotencyKey: "budget-over", Budget: skill.Budget{MaxSteps: 193, MaxSeconds: 1200, MaxFrames: 36}}); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("step cap: %v", err)
	}
	if _, err = svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "frames", IdempotencyKey: "budget-frames", Budget: skill.Budget{MaxSteps: 48, MaxSeconds: 1200, MaxFrames: 129}}); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("frame cap: %v", err)
	}
	capped, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "at cap", IdempotencyKey: "budget-cap", Budget: skill.Budget{MaxSteps: 192, MaxSeconds: 7200, MaxFrames: 128}})
	if err != nil || capped.Budget.MaxSteps != 192 || capped.Budget.MaxFrames != 128 || capped.Remaining.Seconds != 7200 {
		t.Fatalf("accepted cap: %+v %v", capped.Budget, err)
	}
	edit, _, err = svc.ExecuteSkillTool(ctx, "agent:test", edit.ID, skill.ToolRequest{Name: "media.list", Arguments: json.RawMessage(`{}`), ExpectedVersion: edit.Version, IdempotencyKey: "read-list"})
	if err != nil {
		t.Fatal(err)
	}
	edit, _, err = svc.ExecuteSkillTool(ctx, "agent:test", edit.ID, skill.ToolRequest{Name: "artifacts.list", Arguments: json.RawMessage(`{}`), ExpectedVersion: edit.Version, IdempotencyKey: "read-artifacts"})
	if err != nil || edit.Steps != 0 || edit.Remaining.Steps != edit.Budget.MaxSteps {
		t.Fatalf("reads spent steps: %+v %v", edit, err)
	}
	body, _ := json.Marshal(skill.Plan{Body: "Opening", Segments: []skill.Segment{{MediaID: v.Media[0].ID, Start: 0, End: 1, Narration: "Hello"}}})
	edit, _, err = svc.ExecuteSkillTool(ctx, "agent:test", edit.ID, skill.ToolRequest{Name: "scripts.propose", Arguments: body, ExpectedVersion: edit.Version, IdempotencyKey: "write-plan"})
	if err != nil || edit.Steps != 1 || edit.Remaining.Steps != edit.Budget.MaxSteps-1 {
		t.Fatalf("write step: %+v %v", edit, err)
	}
	got, err := svc.GetSkillRun(ctx, "agent:test", edit.ID)
	if err != nil || got.Remaining.Steps != edit.Remaining.Steps || got.Remaining.Frames != edit.Budget.MaxFrames {
		t.Fatalf("get remaining: %+v %v", got.Remaining, err)
	}
}

func TestDrawRunUsesHigherBudget(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Name: "Draw budget", Path: filepath.Join(root, "draw-budget.avp")})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := svc.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateSkillRun(ctx, "agent:draw", skill.RunRequest{ProjectID: view.Document.Project.ID, SequenceID: view.Document.Sequences[0].ID, SkillID: "vac-draw-pipeline", Mode: "external_mcp", Strategy: "draw", VisibleSHA: visible.SHA, Goal: "draw budget", IdempotencyKey: "draw-budget"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Budget.MaxSteps != 128 || run.Budget.MaxSeconds != 3600 || run.Budget.MaxFrames != 96 || run.Remaining.Steps != 128 {
		t.Fatalf("draw budget: %+v remaining %+v", run.Budget, run.Remaining)
	}
	run, _, err = svc.ExecuteSkillTool(ctx, "agent:draw", run.ID, skill.ToolRequest{Name: "draw.list", Arguments: json.RawMessage(`{}`), ExpectedVersion: run.Version, IdempotencyKey: "draw-list"})
	if err != nil || run.Steps != 0 || run.Remaining.Steps != 128 {
		t.Fatalf("draw list spent a step: %+v %v", run, err)
	}
}

func TestVoiceSynthesizeRejectsClosedProjectAndBadRequests(t *testing.T) {
	ctx := t.Context()
	svc, p, v := projectFixture(t)
	run, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "voice", IdempotencyKey: "voice-closed"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"text": "你好", "settings": tts.Settings{Engine: "sapi", Voice: "fixture", Language: "zh", Speed: 1, Volume: 1}})
	run, _, err = svc.ExecuteSkillTool(ctx, "agent:test", run.ID, skill.ToolRequest{Name: "voice.synthesize", Arguments: body, ExpectedVersion: run.Version, IdempotencyKey: "voice-no-project"})
	if err == nil || run.Steps != 1 || run.Remaining.Steps != run.Budget.MaxSteps-1 {
		t.Fatalf("closed project synthesized: %+v %v", run, err)
	}

	workspace, root := avpWorkspaceFixture(t)
	if err = workspace.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := workspace.ProjectFileCommand(ctx, "new", service.ProjectCommand{Name: "Voice", Path: filepath.Join(root, "voice.avp")})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := workspace.ProjectVisibleSet(ctx, view.Document.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := workspace.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: view.Document.Project.ID, SequenceID: view.Document.Sequences[0].ID, SkillID: "vac-draw-pipeline", Mode: "external_mcp", Strategy: "draw", VisibleSHA: visible.SHA, Goal: "narrate", IdempotencyKey: "voice-open"})
	if err != nil {
		t.Fatal(err)
	}
	before := len(view.Document.Resources)
	empty, _ := json.Marshal(map[string]any{"text": "  ", "settings": tts.Settings{Engine: "sapi", Voice: "fixture", Language: "zh", Speed: 1, Volume: 1}})
	opened, _, err = workspace.ExecuteSkillTool(ctx, "agent:test", opened.ID, skill.ToolRequest{Name: "voice.synthesize", Arguments: empty, ExpectedVersion: opened.Version, IdempotencyKey: "voice-empty"})
	if !errors.Is(err, model.ErrArgument) || opened.State != "queued" || opened.Steps != 1 {
		t.Fatalf("empty narration: %+v %v", opened, err)
	}
	external, _ := json.Marshal(map[string]any{"text": "云端旁白", "settings": tts.Settings{Engine: "stepfun", Endpoint: "https://api.stepfun.com/step_plan/v1", Model: "stepaudio-2.5-tts", TokenEnv: "VAC_TTS_TOKEN", Voice: "vibrant-youth", Language: "zh", Speed: 1, Volume: 1}})
	opened, _, err = workspace.ExecuteSkillTool(ctx, "agent:test", opened.ID, skill.ToolRequest{Name: "voice.synthesize", Arguments: external, ExpectedVersion: opened.Version, IdempotencyKey: "voice-cloud"})
	if !errors.Is(err, model.ErrForbidden) || opened.Steps != 2 {
		t.Fatalf("cloud voice without policy: %+v %v", opened, err)
	}
	current, err := workspace.ProjectWorkspace(ctx)
	if err != nil || len(current.Document.Resources) != before {
		t.Fatalf("failed synthesis imported a resource: %d %v", len(current.Document.Resources), err)
	}
}
