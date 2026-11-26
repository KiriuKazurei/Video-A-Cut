package service_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func programClip(t *testing.T, path, color string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c="+color+":s=160x90:r=30:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast", "-threads", "1", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
}

func configureProgramRuntime(t *testing.T, svc *service.Service) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := wd
	for {
		if _, err = os.Stat(filepath.Join(root, "workers", "python", "vac_worker", "script_workbench.py")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("workers/python not found")
		}
		root = parent
	}
	if err = svc.ConfigureTTS("python", filepath.Join(root, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
}

func programProject(t *testing.T) (*service.Service, string, string, string) {
	t.Helper()
	svc, root := avpWorkspaceFixture(t)
	configureProgramRuntime(t, svc)
	ctx := t.Context()
	if err := svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	allow := filepath.Join(root, "allow")
	if err := os.Mkdir(allow, 0700); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureIngest([]ingest.Root{{ID: "media", Name: "Media", Path: allow}}, nil); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "edit.avp"), Name: "Edit"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, root, view.Document.Project.ID, view.Document.Sequences[0].ID
}

func programRun(t *testing.T, svc *service.Service, projectID, sequenceID, skillID, flow, strategy, key string, frames int) skill.Run {
	t.Helper()
	visible, err := svc.ProjectVisibleSet(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateSkillRun(t.Context(), "agent:editor", skill.RunRequest{
		ProjectID: projectID, SequenceID: sequenceID, SkillID: skillID, Flow: flow, Strategy: strategy,
		Mode: "external_mcp", VisibleSHA: visible.SHA, Goal: "Finish one program", IdempotencyKey: key,
		Budget: skill.Budget{MaxSteps: 24, MaxSeconds: 300, MaxFrames: frames},
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func programTool(t *testing.T, svc *service.Service, run skill.Run, name, key string, args any) (skill.Run, map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := svc.ExecuteSkillTool(t.Context(), "agent:editor", run.ID, skill.ToolRequest{
		Name: name, Arguments: raw, ExpectedVersion: run.Version, IdempotencyKey: key,
	})
	body := map[string]any{}
	if len(result) > 0 && result[0] == '{' {
		_ = json.Unmarshal(result, &body)
	}
	return next, body, err
}

func programMedia(t *testing.T, svc *service.Service, run skill.Run, key string) (skill.Run, []skill.Media) {
	t.Helper()
	next, result, err := svc.ExecuteSkillTool(t.Context(), "agent:editor", run.ID, skill.ToolRequest{
		Name: "media.list", Arguments: json.RawMessage(`{}`), ExpectedVersion: run.Version, IdempotencyKey: key,
	})
	var rows []skill.Media
	if err != nil || json.Unmarshal(result, &rows) != nil {
		t.Fatalf("media.list: %s %v", result, err)
	}
	return next, rows
}

func TestSkillAssetImportIsAllowlistedDedupedAndFrozen(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	svc, root, projectID, sequenceID := programProject(t)
	ctx := t.Context()
	allow := filepath.Join(root, "allow")
	inside := filepath.Join(allow, "inside.mp4")
	programClip(t, inside, "red")
	same, err := os.ReadFile(inside)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(allow, "copy.mp4")
	if err = os.WriteFile(copyPath, same, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	programClip(t, outside, "blue")
	seed := filepath.Join(t.TempDir(), "seed.mp4")
	programClip(t, seed, "green")
	if _, err = svc.ImportProjectResource(ctx, projectID, seed); err != nil {
		t.Fatal(err)
	}
	run := programRun(t, svc, projectID, sequenceID, "video-use", "lightweight", "edit", "import-run", 4)
	run, body, err := programTool(t, svc, run, "assets.import", "import-root", map[string]any{"root_id": "media", "relative_path": "inside.mp4"})
	if err != nil || body["imported"] != true || body["deduped"] != false {
		t.Fatalf("allow-list import: %+v %v", body, err)
	}
	imported := body["resource"].(map[string]any)
	if imported["kind"] != "video" || imported["id"] == "" || imported["sha256"] == "" {
		t.Fatalf("import view: %+v", body)
	}
	run, rows := programMedia(t, svc, run, "list-imported")
	if len(rows) != 2 || !mediaHas(rows, imported["id"].(string)) {
		t.Fatalf("frozen set: %+v", rows)
	}
	before, err := svc.ProjectResources(projectID)
	if err != nil {
		t.Fatal(err)
	}
	run, body, err = programTool(t, svc, run, "assets.import", "import-copy", map[string]any{"path": copyPath})
	if err != nil || body["deduped"] != true || body["imported"] != false || body["resource"].(map[string]any)["id"] != imported["id"] {
		t.Fatalf("hash dedup: %+v %v", body, err)
	}
	after, err := svc.ProjectResources(projectID)
	if err != nil || len(after) != len(before) {
		t.Fatalf("dedup created another resource: %d %d %v", len(before), len(after), err)
	}
	run, _, err = programTool(t, svc, run, "assets.import", "import-library", map[string]any{"library_id": "texture-paper", "library_version": "1"})
	if !errors.Is(err, model.ErrArgument) || run.State != "queued" {
		t.Fatalf("unpublished library entry: %+v %v", run, err)
	}
	run, body, err = programTool(t, svc, run, "assets.import", "import-outside", map[string]any{"path": outside})
	if !errors.Is(err, model.ErrInvalidState) || run.State != "waiting_human" || run.Reason != "import_approval_required" {
		t.Fatalf("outside path: state=%s reason=%s err=%v", run.State, run.Reason, err)
	}
	decision, _ := body["decision"].(map[string]any)
	if decision["code"] != "outside_authorized_scope" || decision["name"] != "outside.mp4" {
		t.Fatalf("decision: %+v", body)
	}
	resources, err := svc.ProjectResources(projectID)
	if err != nil || len(resources) != len(before) {
		t.Fatalf("outside path was imported: %d %v", len(resources), err)
	}
	link := filepath.Join(allow, "escape.mp4")
	if err = os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	run, err = svc.UpdateSkillRun(ctx, "agent:editor", run.ID, "resume", run.Version)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = programTool(t, svc, run, "assets.import", "import-link", map[string]any{"root_id": "media", "relative_path": "escape.mp4"})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("symlink escape: %v", err)
	}
	run, err = svc.UpdateSkillRun(ctx, "agent:editor", run.ID, "resume", run.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ImportProjectResource(ctx, projectID, outside); err != nil {
		t.Fatal(err)
	}
	run, body, err = programTool(t, svc, run, "assets.import", "import-approved", map[string]any{"path": outside})
	if err != nil || body["deduped"] != true {
		t.Fatalf("approved retry: %+v %v", body, err)
	}
	approved := body["resource"].(map[string]any)
	if approved["id"] == imported["id"] || approved["sha256"] == imported["sha256"] {
		t.Fatalf("approved file should be the new blue asset: %+v", approved)
	}
	_, rows = programMedia(t, svc, run, "list-approved")
	found := false
	for _, row := range rows {
		if row.ID == approved["id"].(string) {
			found = true
		}
	}
	if len(rows) != 3 || !found || !mediaHas(rows, imported["id"].(string)) {
		t.Fatalf("frozen set after approval: %+v", rows)
	}
}

func mediaHas(rows []skill.Media, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func TestSequenceFramesRejectBudgetIsolationAndUnreviewedFinish(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	svc, _, projectID, sequenceID := programProject(t)
	ctx := t.Context()
	source := filepath.Join(t.TempDir(), "a.mp4")
	programClip(t, source, "red")
	view, err := svc.ImportProjectResource(ctx, projectID, source)
	if err != nil {
		t.Fatal(err)
	}
	resource := view.Document.Resources[0]
	run := programRun(t, svc, projectID, sequenceID, "video-use", "lightweight", "edit", "frame-guards", 1)
	current, err := svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = programTool(t, svc, run, "timeline.edit", "insert-a", map[string]any{"sequence_id": sequenceID, "command": project.Command{
		Session: current.Session, ExpectedRevision: current.Sequence.Revision, IdempotencyKey: "insert-a",
		Operations: []project.Operation{{Type: "clip.insert", TrackID: current.Sequence.Tracks[0].ID, ResourceID: resource.ID}},
	}})
	if err != nil || !run.StructuredEdit {
		t.Fatalf("insert: %+v %v", run, err)
	}
	run, _, err = programTool(t, svc, run, "sequence.frames", "other-sequence", map[string]any{"sequence_id": "seq_other", "frames": []int{0}})
	if !errors.Is(err, model.ErrForbidden) || run.Reason != "authorization_revoked" || run.FramesSpent != 0 {
		t.Fatalf("other sequence: spent=%d reason=%s %v", run.FramesSpent, run.Reason, err)
	}
	run, err = svc.UpdateSkillRun(ctx, "agent:editor", run.ID, "resume", run.Version)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = programTool(t, svc, run, "sequence.frames", "over-budget", map[string]any{"frames": []int{0, 1}})
	if !errors.Is(err, model.ErrInvalidState) || run.Reason != "frame_budget_exceeded" || run.FramesSpent != 0 {
		t.Fatalf("budget: spent=%d reason=%s %v", run.FramesSpent, run.Reason, err)
	}
	run, err = svc.UpdateSkillRun(ctx, "agent:editor", run.ID, "resume", run.Version)
	if err != nil {
		t.Fatal(err)
	}
	current, err = svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = programTool(t, svc, run, "run.finish", "review-early", map[string]any{"sequence_revision": current.Sequence.Revision, "content_version": current.ContentVersion, "review": true})
	if !errors.Is(err, model.ErrArgument) || run.State != "queued" || run.Reason != "invalid_tool_arguments" {
		t.Fatalf("review without frames: state=%s reason=%s %v", run.State, run.Reason, err)
	}
	other := filepath.Join(t.TempDir(), "b.mp4")
	programClip(t, other, "blue")
	if _, err = svc.ImportProjectResource(ctx, projectID, other); err != nil {
		t.Fatal(err)
	}
	resources, err := svc.ProjectResources(projectID)
	if err != nil {
		t.Fatal(err)
	}
	extra := resources[len(resources)-1]
	current, err = svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = programTool(t, svc, run, "timeline.edit", "insert-b", map[string]any{"sequence_id": sequenceID, "command": project.Command{
		Session: current.Session, ExpectedRevision: current.Sequence.Revision, IdempotencyKey: "insert-b",
		Operations: []project.Operation{{Type: "clip.insert", TrackID: current.Sequence.Tracks[1].ID, ResourceID: extra.ID, At: 30}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = programTool(t, svc, run, "sequence.frames", "outside-visible", map[string]any{"frames": []int{0}})
	if !errors.Is(err, model.ErrForbidden) || run.FramesSpent != 0 {
		t.Fatalf("unfrozen media: spent=%d %v", run.FramesSpent, err)
	}
}

func TestSequenceFramesInspectsComposedProgramBeforeReviewFinish(t *testing.T) {
	for _, name := range []string{"ffmpeg", "node"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " not installed")
		}
	}
	svc, _, projectID, sequenceID := programProject(t)
	ctx := t.Context()
	source := filepath.Join(t.TempDir(), "program.mp4")
	programClip(t, source, "red")
	view, err := svc.ImportProjectResource(ctx, projectID, source)
	if err != nil {
		t.Fatal(err)
	}
	resource := view.Document.Resources[0]
	run := programRun(t, svc, projectID, sequenceID, "video-use", "lightweight", "edit", "frame-delivery", 2)
	current, err := svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = programTool(t, svc, run, "timeline.edit", "insert", map[string]any{"sequence_id": sequenceID, "command": project.Command{
		Session: current.Session, ExpectedRevision: current.Sequence.Revision, IdempotencyKey: "insert",
		Operations: []project.Operation{{Type: "clip.insert", TrackID: current.Sequence.Tracks[0].ID, ResourceID: resource.ID}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	current, err = svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	run, body, err := programTool(t, svc, run, "sequence.frames", "frame-0", map[string]any{
		"frames": []int{0}, "sequence_revision": current.Sequence.Revision, "content_version": current.ContentVersion,
	})
	if err != nil || run.FramesSpent != 1 || body["content_version"] != current.ContentVersion {
		t.Fatalf("program frame: spent=%d body=%+v %v", run.FramesSpent, body, err)
	}
	frames := body["frames"].([]any)
	frame := frames[0].(map[string]any)
	if frame["strategy"] != "sequence:"+current.ContentVersion || frame["media_id"] != sequenceID {
		t.Fatalf("frame identity: %+v", frame)
	}
	artifact := body["artifacts"].([]any)[0].(map[string]any)
	run, pngBody, err := programTool(t, svc, run, "artifacts.get", "read-frame", map[string]any{"id": artifact["id"]})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(pngBody["base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(decoded))
	if err != nil || im.Bounds().Dx() != current.Sequence.Width {
		t.Fatalf("composed png: %v %v", im, err)
	}
	red, green, blue, _ := im.At(im.Bounds().Dx()/2, im.Bounds().Dy()/2).RGBA()
	if red < 40000 || green > 8000 || blue > 8000 {
		t.Fatalf("composed frame is not the red program: %d %d %d", red, green, blue)
	}
	fresh, err := svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	run, body, err = programTool(t, svc, run, "run.finish", "review-finish", map[string]any{
		"sequence_revision": fresh.Sequence.Revision, "content_version": fresh.ContentVersion, "review": true,
	})
	if err != nil || run.State != "succeeded" || body["reviewed"] != true || body["human_acceptance"] != "not_checked" {
		t.Fatalf("review finish: state=%s reason=%s stored=%s body=%+v %v", run.State, run.Reason, run.SequenceContentVersion, body, err)
	}
	ids, _ := body["program_frame_ids"].([]any)
	if len(ids) != 1 || ids[0] != frame["id"] {
		t.Fatalf("finish did not keep the inspected program frame: %+v", body["program_frame_ids"])
	}
}
