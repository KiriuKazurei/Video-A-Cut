package service_test

import (
	"encoding/json"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSkillRealMediaFramesAndDelivery(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	svc, _, media := scriptFixture(t)
	ctx := t.Context()
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30:duration=3", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", media[0].Path)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("media fixture: %v %s", err, raw)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureTTS("python", filepath.Join(root, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
	if err = svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := svc.SaveProject(ctx, "human:test", "", service.ProjectRequest{Name: "Real", Flow: "visual_heavy", Assets: []string{"clip"}, Visible: []string{"clip"}, AcceptExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.ProjectVisibleSet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ConfirmProjectVisible(ctx, "human:test", p.ID, v.SHA, p.Version); err != nil {
		t.Fatal(err)
	}
	r, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "vac-visual-summary", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "Show scene", IdempotencyKey: "real"})
	if err != nil {
		t.Fatal(err)
	}
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "media.get_frames", Arguments: json.RawMessage("{}"), ExpectedVersion: r.Version, IdempotencyKey: "frames"})
	if err != nil || len(r.Frames) < 2 {
		t.Fatalf("frames %+v %v", r, err)
	}
	summary, _ := json.Marshal(map[string]any{
		"model": "external-fixture", "narrative": "Synthetic test scene", "beats": []string{"Opening"},
		"timeline": []any{map[string]any{"media_id": v.Media[0].ID, "start": 0, "end": 1,
			"description": "Evidence frame", "confidence": "low", "frame_ids": []string{r.Frames[0].ID}}},
	})
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "summaries.submit", Arguments: summary, ExpectedVersion: r.Version, IdempotencyKey: "summary"})
	if err != nil || len(r.Summary) == 0 {
		t.Fatalf("visual summary: %v", err)
	}
	summaries := 0
	for _, artifact := range r.Artifacts {
		if artifact.Kind == "summary" {
			summaries++
			if _, _, err := svc.SkillArtifactPath(ctx, "human:test", r.ID, artifact.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if summaries != 2 {
		t.Fatalf("expected JSON and Markdown summaries, got %d", summaries)
	}
	plan := skill.Plan{Body: "Show scene", Segments: []skill.Segment{{MediaID: v.Media[0].ID, Start: 0, End: 1, Narration: "Scene"}}}
	raw, _ := json.Marshal(plan)
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "scripts.propose", Arguments: raw, ExpectedVersion: r.Version, IdempotencyKey: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	r, err = svc.UpdateSkillRun(ctx, "human:test", r.ID, "finalize", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "renders.create", ExpectedVersion: r.Version, IdempotencyKey: "render"})
	if err != nil {
		t.Fatal(err)
	}
	var mp4 skill.Artifact
	for _, a := range r.Artifacts {
		if a.Kind == "mp4" {
			mp4 = a
		}
	}
	path, _, err := svc.SkillArtifactPath(ctx, "human:test", r.ID, mp4.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() < 100 {
		t.Fatalf("missing MP4 %v", err)
	}
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "renders.inspect", ExpectedVersion: r.Version, IdempotencyKey: "inspect"})
	if err != nil || r.Inspection == nil || len(r.Inspection.FrameIDs) < 2 {
		t.Fatalf("render inspection: %+v %v", r.Inspection, err)
	}
	if err = os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.SkillArtifactPath(ctx, "human:test", r.ID, mp4.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("tampered media remained downloadable: %v", err)
	}
}

func TestSkillToolDraftGateReplayAndCancellation(t *testing.T) {
	svc, p, v := projectFixture(t)
	ctx := t.Context()
	r, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "draft", IdempotencyKey: "new"})
	if err != nil {
		t.Fatal(err)
	}
	plan := skill.Plan{Body: "Describe source", Segments: []skill.Segment{{MediaID: v.Media[0].ID, Start: 0, End: 1, Narration: "source"}}}
	raw, _ := json.Marshal(plan)
	req := skill.ToolRequest{Name: "scripts.propose", Arguments: raw, ExpectedVersion: r.Version, IdempotencyKey: "propose"}
	r, result, err := svc.ExecuteSkillTool(ctx, "agent:test", r.ID, req)
	if err != nil || r.State != "waiting_human" {
		t.Fatalf("draft: %+v %v", r, err)
	}
	before := r.Version
	again, replayed, err := svc.ExecuteSkillTool(ctx, "agent:test", r.ID, req)
	if err != nil || string(result) != string(replayed) || again.Version != before {
		t.Fatalf("replay %v", err)
	}
	if _, err = svc.UpdateSkillRun(ctx, "agent:test", r.ID, "finalize", r.Version); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("agent approved: %v", err)
	}
	r, err = svc.UpdateSkillRun(ctx, "human:test", r.ID, "finalize", r.Version)
	if err != nil || r.Plan.FinalizedBy != "human:test" {
		t.Fatal(err)
	}
	r, err = svc.UpdateSkillRun(ctx, "agent:test", r.ID, "cancel", r.Version)
	if err != nil || r.State != "cancelled" {
		t.Fatal(err)
	}
	_, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "media.list", ExpectedVersion: r.Version, IdempotencyKey: "after-cancel"})
	if !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("cancelled run executed: %v", err)
	}
}

func projectFixture(t *testing.T) (*service.Service, skill.Project, skill.VisibleSet) {
	t.Helper()
	svc, _, _ := scriptFixture(t)
	ctx := t.Context()
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := svc.SaveProject(ctx, "human:test", "", service.ProjectRequest{Name: "Project", Flow: "lightweight", Assets: []string{"clip"}, Visible: []string{"clip"}, AcceptExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.ProjectVisibleSet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ConfirmProjectVisible(ctx, "agent:test", p.ID, v.SHA, p.Version); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("agent confirmed: %v", err)
	}
	v, err = svc.ConfirmProjectVisible(ctx, "human:test", p.ID, v.SHA, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return svc, p, v
}
func TestSkillProjectIsolationAndImmutableSnapshot(t *testing.T) {
	svc, p, v := projectFixture(t)
	ctx := t.Context()
	req := skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "Make a short video", IdempotencyKey: "same"}
	r, err := svc.CreateSkillRun(ctx, "agent:test", req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.CreateSkillRun(ctx, "agent:test", req)
	if err != nil || again.ID != r.ID {
		t.Fatalf("replay: %v", err)
	}
	req.Goal = "Different"
	if _, err = svc.CreateSkillRun(ctx, "agent:test", req); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("reused key: %v", err)
	}
	if _, err = svc.GetSkillRun(ctx, "agent:other", r.ID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("owner: %v", err)
	}
	if _, err = svc.UpdateSkillRun(ctx, "agent:test", r.ID, "finalize", r.Version); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("fake human: %v", err)
	}
	if _, err = svc.EnableSkill(ctx, "human:test", "video-use", false); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SkillRunContext(ctx, "agent:test", r.ID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("disabled: %v", err)
	}
}
func TestSkillProjectRequiresFreshConfirmation(t *testing.T) {
	svc, p, v := projectFixture(t)
	ctx := t.Context()
	_, err := svc.SaveProject(ctx, "human:test", p.ID, service.ProjectRequest{ExpectedVersion: p.Version, Name: p.Name, Flow: p.Flow, Assets: p.Assets, Visible: p.Visible, AcceptExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "draft", IdempotencyKey: "fresh"})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("stale confirmation: %v", err)
	}
}

func TestSkillClaimChecksNewAgentRoleBeforeTransfer(t *testing.T) {
	svc, p, v := projectFixture(t)
	ctx := t.Context()
	r, err := svc.CreateSkillRun(ctx, "human:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", VisibleSHA: v.SHA, Goal: "draft", IdempotencyKey: "claim"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimSkillRun(ctx, "agent:reviewer", r.ID, r.Version, "unrelated-role"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("disallowed role claimed run: %v", err)
	}
	claimed, err := svc.ClaimSkillRun(ctx, "agent:narrator", r.ID, r.Version, "narrator")
	if err != nil || claimed.Owner != "agent:narrator" {
		t.Fatalf("valid claim: %+v %v", claimed, err)
	}
}
