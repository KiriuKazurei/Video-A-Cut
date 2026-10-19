package service_test

import (
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
	"testing"
)

func TestSkillUndoRedoBranchesAndCannotRestoreApprovalOrBudget(t *testing.T) {
	svc, p, v := projectFixture(t)
	ctx := t.Context()
	r, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "draft", IdempotencyKey: "history"})
	if err != nil {
		t.Fatal(err)
	}
	plan := skill.Plan{Body: "Describe", Segments: []skill.Segment{{MediaID: v.Media[0].ID, Start: 0, End: 1, Narration: "A"}}}
	r, err = svc.SaveSkillPlan(ctx, "human:test", r.ID, service.SkillPlanSave{ExpectedVersion: r.Version, Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	plan.Segments[0].Narration = "B"
	r, err = svc.SaveSkillPlan(ctx, "human:test", r.ID, service.SkillPlanSave{ExpectedVersion: r.Version, Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	before := r.Version
	beforePlan := r.Plan.Version
	steps := r.Steps
	elapsed := r.ElapsedMilliseconds
	r, err = svc.MoveSkillHistory(ctx, "human:test", r.ID, "undo", r.Version)
	if err != nil || r.Plan.Segments[0].Narration != "A" || r.Plan.Version <= beforePlan || r.Plan.FinalizedBy != "" || r.Version <= before || r.Steps != steps || r.ElapsedMilliseconds != elapsed {
		t.Fatalf("undo %+v %v", r, err)
	}
	r, err = svc.MoveSkillHistory(ctx, "human:test", r.ID, "redo", r.Version)
	if err != nil || r.Plan.Segments[0].Narration != "B" {
		t.Fatalf("redo %+v %v", r, err)
	}
	r, err = svc.MoveSkillHistory(ctx, "human:test", r.ID, "undo", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	plan.Segments[0].Narration = "C"
	r, err = svc.SaveSkillPlan(ctx, "human:test", r.ID, service.SkillPlanSave{ExpectedVersion: r.Version, Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.MoveSkillHistory(ctx, "human:test", r.ID, "redo", r.Version); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("obsolete redo survived %v", err)
	}
	history, err := svc.SkillEditHistory(ctx, "human:test", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history["checkpoints"].([]store.EditCheckpoint)) < 4 {
		t.Fatal("old branch deleted")
	}
	if _, err = svc.SkillEditHistory(ctx, "agent:test", r.ID); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("Agent read history", err)
	}
	if _, err = svc.AgentOperations(ctx, "agent:test", r.ID, 0, 100); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("Agent read journal", err)
	}
	if _, err = svc.MoveSkillHistory(ctx, "agent:test", r.ID, "undo", r.Version); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("Agent rewound history", err)
	}
	if _, err = svc.VerifyAgentOperations(ctx, "human:test"); err != nil {
		t.Fatal(err)
	}
}

func TestSkillUndoDoesNotResurrectRevokedVoiceConsent(t *testing.T) {
	svc, p, v := projectFixture(t)
	ctx := t.Context()
	r, e := svc.CreateSkillRun(ctx, "human:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", VisibleSHA: v.SHA, Goal: "voice", IdempotencyKey: "voice-history"})
	if e != nil {
		t.Fatal(e)
	}
	plan := skill.Plan{Body: "Opening", Segments: []skill.Segment{{MediaID: v.Media[0].ID, Start: 0, End: 1, Narration: "Hello"}}}
	r, e = svc.SaveSkillPlan(ctx, "human:test", r.ID, service.SkillPlanSave{ExpectedVersion: r.Version, Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	settings := tts.Settings{Engine: "stepfun", Endpoint: "https://tts.example.invalid", Model: "stepaudio-2.5-tts", TokenEnv: "UNUSED_VOICE_KEY", Voice: "fixture", Language: "en", Speed: 1, Volume: 1}
	r, e = svc.SaveSkillVoice(ctx, "human:test", r.ID, service.SkillVoiceRequest{ExpectedVersion: r.Version, SegmentIndex: 0, Enabled: true, Settings: settings, AcceptExternal: true})
	if e != nil {
		t.Fatal(e)
	}
	r, e = svc.SaveSkillVoice(ctx, "human:test", r.ID, service.SkillVoiceRequest{ExpectedVersion: r.Version, SegmentIndex: 0, Enabled: false, Settings: settings, AcceptExternal: false})
	if e != nil {
		t.Fatal(e)
	}
	r, e = svc.MoveSkillHistory(ctx, "human:test", r.ID, "undo", r.Version)
	if e != nil || !r.Plan.Segments[0].TTSEnabled || r.Plan.Segments[0].VoiceConsent {
		t.Fatalf("undo revived voice consent: %+v %v", r.Plan, e)
	}
	r, e = svc.UpdateSkillRun(ctx, "human:test", r.ID, "finalize", r.Version)
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = svc.ExecuteSkillTool(ctx, "human:test", r.ID, skill.ToolRequest{Name: "renders.create", ExpectedVersion: r.Version, IdempotencyKey: "must-not-send"})
	if !errors.Is(e, model.ErrForbidden) {
		t.Fatalf("external voice was not blocked before request: %v", e)
	}
}
