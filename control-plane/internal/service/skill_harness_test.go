package service_test

import (
	"encoding/json"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExternalAgentProgressRenewsLeaseAndKeepsOwnership(t *testing.T) {
	svc, p, v := projectFixture(t)
	clock := newFakeClock()
	svc.SetClock(clock.Now)
	ctx := t.Context()
	r, err := svc.CreateSkillRun(ctx, "human:test", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", VisibleSHA: v.SHA, Goal: "draw", IdempotencyKey: "progress"})
	if err != nil {
		t.Fatal(err)
	}
	r, err = svc.ClaimSkillRun(ctx, "agent:narrator", r.ID, r.Version, "narrator")
	if err != nil {
		t.Fatal(err)
	}
	firstLease := *r.LeaseUntil
	clock.Advance(90 * time.Second)
	r, err = svc.ReportSkillProgress(ctx, "agent:narrator", r.ID, r.Version, "generating", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !r.LeaseUntil.After(firstLease) || r.Progress == nil || r.Progress.Phase != "generating" || r.Progress.OutputChars != 100 || r.Steps != 0 {
		t.Fatalf("progress/lease: %+v", r)
	}
	if _, err = svc.ReportSkillProgress(ctx, "agent:other", r.ID, r.Version, "generating", 200); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("another agent: %v", err)
	}
	if _, err = svc.ReportSkillProgress(ctx, "agent:narrator", r.ID, r.Version, "invalid", 200); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("invalid phase: %v", err)
	}
	if _, err = svc.ReportSkillProgress(ctx, "agent:narrator", r.ID, r.Version-1, "generating", 100); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale heartbeat accepted: %v", err)
	}
	clock.Advance(90 * time.Second)
	r, err = svc.ReportSkillProgress(ctx, "agent:narrator", r.ID, r.Version, "generating", 200)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:narrator", r.ID, skill.ToolRequest{Name: "media.list", Arguments: json.RawMessage(`{}`), ExpectedVersion: r.Version, IdempotencyKey: "after-progress"})
	if err != nil {
		t.Fatalf("long generation lost its lease: %v", err)
	}
	if r.Steps != 1 {
		t.Fatalf("heartbeats must not consume tool budget: %+v", r)
	}
	clock.Advance(3 * time.Minute)
	if _, err = svc.ReportSkillProgress(ctx, "agent:narrator", r.ID, r.Version, "generating", 300); !errors.Is(err, model.ErrLeaseExpired) {
		t.Fatalf("expired lease renewed: %v", err)
	}
}

func TestSkillRealMediaFramesAndDelivery(t *testing.T) { runSkillMediaDelivery(t, false, false) }

func TestProjectRealMediaFramesAndDelivery(t *testing.T) { runSkillMediaDelivery(t, true, false) }

func TestSkillBlackWarningDoesNotBlockCompletion(t *testing.T) { runSkillMediaDelivery(t, true, true) }

func runSkillMediaDelivery(t *testing.T, native, black bool) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	svc, _, media := scriptFixture(t)
	ctx := t.Context()
	visual := "testsrc2=size=160x90:rate=30:duration=3"
	if black {
		visual = "color=c=black:s=160x90:r=30:d=3"
	}
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", visual, "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", media[0].Path)
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
	var p skill.Project
	var v skill.VisibleSet
	if native {
		if err = svc.ConfigureProjectWorkspace(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(svc.CloseProjectWorkspace)
		view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(t.TempDir(), "real.avp"), Name: "Native media"})
		if e != nil {
			t.Fatal(e)
		}
		original := filepath.Join(t.TempDir(), "a.mp4")
		data, e := os.ReadFile(media[0].Path)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(original, data, 0600); e != nil {
			t.Fatal(e)
		}
		if _, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, original); err != nil {
			t.Fatal(err)
		}
		p, err = svc.GetProject(ctx, view.Document.Project.ID)
		if err != nil {
			t.Fatal(err)
		}
		v, err = svc.ProjectVisibleSet(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		p, err = svc.SaveProject(ctx, "human:test", "", service.ProjectRequest{Name: "Real", Flow: "visual_heavy", Assets: []string{"clip"}, Visible: []string{"clip"}, AcceptExternal: true})
		if err != nil {
			t.Fatal(err)
		}
		v, err = svc.ProjectVisibleSet(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.ConfirmProjectVisible(ctx, "human:test", p.ID, v.SHA, p.Version); err != nil {
			t.Fatal(err)
		}
	}
	r, err := svc.CreateSkillRun(ctx, "agent:test", skill.RunRequest{ProjectID: p.ID, Flow: "visual_heavy", SkillID: "vac-visual-summary", Mode: "external_mcp", AgentRole: "narrator", VisibleSHA: v.SHA, Goal: "Show scene", IdempotencyKey: "real"})
	if err != nil {
		t.Fatal(err)
	}
	var parallelSequence string
	if native {
		view, e := svc.ProjectWorkspace(ctx)
		if e != nil {
			t.Fatal(e)
		}
		timeline, e := svc.Timeline(ctx, p.ID, r.SequenceID)
		if e != nil {
			t.Fatal(e)
		}
		_, e = svc.TimelineCommand(ctx, "human:window", p.ID, r.SequenceID, project.Command{Session: view.ID, ExpectedRevision: timeline.Sequence.Revision, IdempotencyKey: "manual-before-skill", Operations: []project.Operation{{Type: "clip.insert", TrackID: timeline.Sequence.Tracks[0].ID, ResourceID: v.Media[0].ID}}})
		if e != nil {
			t.Fatal(e)
		}
		view, _ = svc.ProjectWorkspace(ctx)
		view, e = svc.ManageSequence(ctx, p.ID, service.SequenceRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "parallel-sequence", Action: "create"})
		if e != nil {
			t.Fatal(e)
		}
		parallelSequence = view.Document.Sequences[1].ID
		if _, e = svc.UpdateProjectWorkspaceState(ctx, view.ID, map[string]any{"active_sequence": parallelSequence}); e != nil {
			t.Fatal(e)
		}
	}
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "media.get_frames", Arguments: json.RawMessage("{}"), ExpectedVersion: r.Version, IdempotencyKey: "frames"})
	if err != nil || len(r.Frames) < 2 {
		t.Fatalf("frames %+v %v", r, err)
	}
	if native {
		secondSource := filepath.Join(t.TempDir(), "b.mp4")
		bytes, e := os.ReadFile(media[0].Path)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(secondSource, bytes, 0600); e != nil {
			t.Fatal(e)
		}
		view, e := svc.ImportProjectResource(ctx, p.ID, secondSource)
		if e != nil {
			t.Fatal(e)
		}
		note := "备注不会使已有成片任务失效"
		renamed := "仅更改显示名称"
		if _, e = svc.UpdateProjectResource(ctx, p.ID, view.Document.Resources[0].ID, service.ResourcePatch{Notes: &note, Name: &renamed}); e != nil {
			t.Fatal(e)
		}
		if _, e = svc.SkillRunContext(ctx, "human:test", r.ID); e != nil {
			t.Fatal("unrelated import/notes/rename invalidated run", e)
		}
	}
	spent := r.FramesSpent
	r, err = svc.MoveSkillHistory(ctx, "human:test", r.ID, "undo", r.Version)
	if err != nil || len(r.Frames) != 0 || r.FramesSpent != spent {
		t.Fatalf("frame undo reset budget: %+v %v", r, err)
	}
	r, err = svc.MoveSkillHistory(ctx, "human:test", r.ID, "redo", r.Version)
	if err != nil || len(r.Frames) < 2 || r.FramesSpent != spent {
		t.Fatalf("frame redo reset budget: %+v %v", r, err)
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
	plan := skill.Plan{Body: "Show opening\n\nShow ending", Segments: []skill.Segment{{MediaID: v.Media[0].ID, Start: 0, End: .5, Narration: "Opening"}, {MediaID: v.Media[0].ID, Start: .5, End: 1, Narration: "Ending"}}}
	if black {
		plan.Segments[0].End = 1
		plan.Segments[1].Start = 1
		plan.Segments[1].End = 2
	}
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
	var inspectResult json.RawMessage
	r, inspectResult, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "renders.inspect", ExpectedVersion: r.Version, IdempotencyKey: "inspect"})
	if err != nil || r.Inspection == nil || len(r.Inspection.FrameIDs) < 2 {
		t.Fatalf("render inspection: %+v %v", r.Inspection, err)
	}
	var checked struct {
		BasicQuality struct {
			SHA256 string `json:"sha256"`
			Status string `json:"status"`
		} `json:"basic_quality"`
	}
	wantQuality := "passed"
	if black {
		wantQuality = "failed"
	}
	if e := json.Unmarshal(inspectResult, &checked); e != nil || checked.BasicQuality.SHA256 != mp4.SHA || checked.BasicQuality.Status != wantQuality {
		t.Fatalf("quality inspection: %s, %v", inspectResult, e)
	}
	if native {
		review := *r.Inspection
		review.Verdict = "passed"
		review.Issues = []string{}
		reviewJSON, _ := json.Marshal(review)
		r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "renders.review", Arguments: reviewJSON, ExpectedVersion: r.Version, IdempotencyKey: "review-before-save"})
		if err != nil {
			t.Fatal(err)
		}
		r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "run.finish", ExpectedVersion: r.Version, IdempotencyKey: "finish-before-save"})
		if err != nil {
			t.Fatal(err)
		}
		if black {
			return
		}
		launch, e := svc.Timeline(ctx, p.ID, r.SequenceID)
		if e != nil {
			t.Fatal(e)
		}
		other, e := svc.Timeline(ctx, p.ID, parallelSequence)
		if e != nil {
			t.Fatal(e)
		}
		if len(launch.Sequence.Groups) != 1 || launch.Sequence.ScopeGroup != launch.Sequence.Groups[0].ID || len(other.Sequence.Groups) != 0 || len(launch.Sequence.Tracks[0].Clips) != 1 {
			t.Fatal("completed MP4 did not preserve manual edit in launch sequence")
		}
		for _, track := range launch.Sequence.Tracks {
			if track.GroupID != "" {
				for _, clip := range track.Clips {
					if clip.At != 0 || clip.Origin.RunID != r.ID || clip.Origin.SHA256 != mp4.SHA {
						t.Fatal("Agent source/position was lost")
					}
				}
			}
		}
		session, _ := svc.ProjectWorkspace(ctx)
		request := service.SequenceResultRequest{Session: session.ID, ExpectedRevision: launch.Sequence.Revision, IdempotencyKey: "repeat-result", RunID: r.ID, ArtifactID: mp4.ID}
		again, e := svc.ImportSequenceSkillResult(ctx, p.ID, r.SequenceID, request)
		if e != nil || again.Sequence.Revision != launch.Sequence.Revision || len(again.Sequence.Groups) != 1 {
			t.Fatalf("completion retry imported twice: %v", e)
		}
		request.ExpectedRevision = other.Sequence.Revision
		request.IdempotencyKey = "history-result"
		historical, e := svc.ImportSequenceSkillResult(ctx, p.ID, parallelSequence, request)
		if e != nil || len(historical.Sequence.Groups) != 1 {
			t.Fatalf("explicit historical import: %v", e)
		}
		current, e := svc.Timeline(ctx, p.ID, r.SequenceID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = svc.TimelineCommand(ctx, "human:window", p.ID, r.SequenceID, project.Command{Session: session.ID, ExpectedRevision: current.Sequence.Revision, IdempotencyKey: "undo-import", Operations: []project.Operation{{Type: "history.undo"}}}); e != nil {
			t.Fatal(e)
		}
		view, e := svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{})
		if e != nil {
			t.Fatal(e)
		}
		originalProject := view.Path
		fresh, _ := avpWorkspaceFixture(t)
		if e = fresh.ConfigureSkillHarness(ctx); e != nil {
			t.Fatal(e)
		}
		if _, e = fresh.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: originalProject}); e != nil {
			t.Fatal(e)
		}
		kept, e := fresh.Timeline(ctx, p.ID, r.SequenceID)
		if e != nil || len(kept.Sequence.Groups) != 0 || len(kept.Sequence.Tracks[0].Clips) != 1 {
			t.Fatalf("recovery reapplied an undone automatic result: %v", e)
		}
		if _, _, e = fresh.SkillArtifactPath(ctx, "human:test", r.ID, mp4.ID); e != nil {
			t.Fatal("fresh cache lost completed output reference", e)
		}
		clone, e := svc.ProjectFileCommand(ctx, "save-as", service.ProjectCommand{Path: filepath.Join(t.TempDir(), "copy.avp")})
		if e != nil {
			t.Fatal(e)
		}
		if clone.Document.Project.ID == p.ID {
			t.Fatal("save-as reused identity")
		}
		if _, _, e = svc.SkillArtifactPath(ctx, "human:test", r.ID, mp4.ID); e != nil {
			t.Fatal("save-as lost completed output", e)
		}
		if _, e = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: originalProject}); e != nil {
			t.Fatal(e)
		}
	}
	index := 0
	r, err = svc.RequestSkillEdit(ctx, "human:test", r.ID, service.SkillEditRequest{ExpectedVersion: r.Version, PlanVersion: r.Plan.Version, Prompt: "Change only the opening narration", SegmentIndex: &index, ArtifactID: mp4.ID, ArtifactSHA: mp4.SHA, IdempotencyKey: "local-edit"})
	if err != nil || r.EditRequest == nil || r.EditRequest.Windows[0].TimelineStart != 0 || r.EditRequest.Windows[0].TimelineEnd != .5 || r.EditRequest.Windows[0].SourceStart != 0 || r.EditRequest.Windows[0].SourceEnd != .5 {
		t.Fatalf("target windows %+v %v", r.EditRequest, err)
	}
	proposed := *r.Plan
	proposed.Segments = append([]skill.Segment{}, r.Plan.Segments...)
	proposed.Segments[1].Narration = "Unauthorized outside rewrite"
	raw, _ = json.Marshal(proposed)
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "scripts.propose", Arguments: raw, ExpectedVersion: r.Version, IdempotencyKey: "outside"})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("accepted unrelated edit: %v", err)
	}
	r, err = svc.UpdateSkillRun(ctx, "human:test", r.ID, "resume", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	proposed.Segments[1] = plan.Segments[1]
	proposed.Segments[0].Narration = "New opening"
	proposed.Segments[0].End = .4
	raw, _ = json.Marshal(proposed)
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "scripts.propose", Arguments: raw, ExpectedVersion: r.Version, IdempotencyKey: "inside"})
	if err != nil || r.Plan.Segments[1].Narration != "Ending" || r.Plan.FinalizedBy != "" {
		t.Fatalf("local edit: %+v %v", r.Plan, err)
	}
	workbench, err := svc.SkillWorkbench(ctx, "human:test", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	previews := workbench["previews"].([]map[string]any)
	if len(previews) == 0 || previews[0]["available"] != true || previews[0]["historical"] != true {
		t.Fatal("old film cannot be previewed during editing", previews)
	}
	checkpoint := previews[0]["checkpoint_id"].(string)
	if _, _, err = svc.SkillCheckpointPreview(ctx, "human:test", r.ID, checkpoint, mp4.ID); err != nil {
		t.Fatal(err)
	}
	r, err = svc.UpdateSkillRun(ctx, "human:test", r.ID, "finalize", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err = svc.ExecuteSkillTool(ctx, "agent:test", r.ID, skill.ToolRequest{Name: "renders.create", ExpectedVersion: r.Version, IdempotencyKey: "edited-render"})
	if err != nil {
		t.Fatal(err)
	}
	var editedMP4 skill.Artifact
	for _, a := range r.Artifacts {
		if a.Kind == "mp4" && a.PlanVersion == r.Plan.Version {
			editedMP4 = a
		}
	}
	if editedMP4.ID == "" || editedMP4.SHA == mp4.SHA {
		t.Fatal("edited video was not regenerated")
	}
	at := .6
	r, err = svc.RequestSkillEdit(ctx, "human:test", r.ID, service.SkillEditRequest{ExpectedVersion: r.Version, PlanVersion: r.Plan.Version, Prompt: "Adjust the ending", At: &at, ArtifactID: editedMP4.ID, ArtifactSHA: editedMP4.SHA, IdempotencyKey: "point-edit"})
	if err != nil || r.EditRequest.Windows[0].SegmentIndex != 1 || r.EditRequest.Windows[0].TimelineStart != .4 || r.EditRequest.Windows[0].SourceStart != .5 {
		t.Fatalf("render/source time mapping %+v %v", r.EditRequest, err)
	}
	if _, err = svc.AgentOperations(ctx, "agent:test", r.ID, 0, 100); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("Agent read logs", err)
	}
	if err = os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.SkillArtifactPath(ctx, "human:test", r.ID, mp4.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("tampered media remained downloadable: %v", err)
	}
	if _, _, err = svc.SkillCheckpointPreview(ctx, "human:test", r.ID, checkpoint, mp4.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatal("historical preview ignored tamper", err)
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
