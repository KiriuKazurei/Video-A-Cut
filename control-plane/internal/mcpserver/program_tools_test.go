package mcpserver

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func programClip(t *testing.T, path, color string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c="+color+":s=160x90:r=30:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast", "-threads", "1", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
}

func programFixture(t *testing.T) (*loopFixture, string, string, string) {
	t.Helper()
	f := newLoopFixture(t, 2*time.Minute, Agent{ID: "editor", Role: "editor"})
	ctx := t.Context()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "delivery"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ConfigureDeliveryRoot(filepath.Join(root, "delivery")); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ConfigureProjectWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.svc.CloseProjectWorkspace)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := wd
	for {
		if _, statErr := os.Stat(filepath.Join(repo, "workers", "python", "vac_worker", "script_workbench.py")); statErr == nil {
			break
		}
		parent := filepath.Dir(repo)
		if parent == repo {
			t.Fatal("workers/python not found")
		}
		repo = parent
	}
	if err = f.svc.ConfigureTTS("python", filepath.Join(repo, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.svc.CloseTTS)
	if err := f.svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	allow := filepath.Join(root, "allow")
	if err := os.Mkdir(allow, 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ConfigureIngest([]ingest.Root{{ID: "media", Name: "Media", Path: allow}}, nil); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "edit.avp"), Name: "Edit"})
	if err != nil {
		t.Fatal(err)
	}
	return f, root, view.Document.Project.ID, view.Document.Sequences[0].ID
}

func mcpRunVersion(t *testing.T, run map[string]any) float64 {
	t.Helper()
	version, ok := run["version"].(float64)
	if !ok {
		t.Fatalf("missing run version: %+v", run)
	}
	return version
}

func TestMCPAssetImportAllowlistDedupAndDecision(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	f, root, projectID, sequenceID := programFixture(t)
	ctx := t.Context()
	inside := filepath.Join(root, "allow", "inside.mp4")
	programClip(t, inside, "red")
	same, err := os.ReadFile(inside)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "allow", "copy.mp4"), same, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.mp4")
	programClip(t, outside, "blue")
	seed := filepath.Join(root, "seed.mp4")
	programClip(t, seed, "green")
	if _, err = f.svc.ImportProjectResource(ctx, projectID, seed); err != nil {
		t.Fatal(err)
	}
	visible, err := f.svc.ProjectVisibleSet(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	created, bad, raw := f.call("editor", "skill_runs.create", map[string]any{
		"project_id": projectID, "sequence_id": sequenceID, "flow": "lightweight", "skill_id": "video-use",
		"strategy": "edit", "execution_mode": "external_mcp", "visible_set_sha": visible.SHA,
		"goal": "Import one authorized file", "idempotency_key": "mcp-import-run",
		"budget": map[string]any{"max_steps": 16, "max_seconds": 120, "max_frames": 4},
	})
	if bad {
		t.Fatalf("create: %s", raw)
	}
	id, _ := created["id"].(string)
	version := mcpRunVersion(t, created)
	call := func(key, name string, args any) (map[string]any, bool, string) {
		t.Helper()
		out, isErr, text := f.call("editor", "skill_runs.tool", map[string]any{"run_id": id, "request": map[string]any{
			"name": name, "arguments": args, "expected_version": version, "idempotency_key": key,
		}})
		if run, ok := out["run"].(map[string]any); ok {
			version = mcpRunVersion(t, run)
		}
		return out, isErr, text
	}
	out, isErr, text := call("import-root", "assets.import", map[string]any{"root_id": "media", "relative_path": "inside.mp4"})
	if isErr {
		t.Fatalf("allow-list import: %s", text)
	}
	result := out["result"].(map[string]any)
	if result["imported"] != true {
		t.Fatalf("import result: %+v", result)
	}
	resourceID := result["resource"].(map[string]any)["id"]
	out, isErr, text = call("import-copy", "assets.import", map[string]any{"path": filepath.Join(root, "allow", "copy.mp4")})
	if isErr || out["result"].(map[string]any)["deduped"] != true || out["result"].(map[string]any)["resource"].(map[string]any)["id"] != resourceID {
		t.Fatalf("dedup: %s", text)
	}
	out, isErr, text = call("import-outside", "assets.import", map[string]any{"path": outside})
	if !isErr {
		t.Fatalf("outside path succeeded: %s", text)
	}
	got, isErr, text := f.call("editor", "skill_runs.get", map[string]any{"run_id": id})
	if isErr {
		t.Fatalf("get: %s", text)
	}
	run := got["run"].(map[string]any)
	if run["state"] != "waiting_human" || run["reason"] != "import_approval_required" {
		t.Fatalf("decision pause: %+v", run)
	}
	resources, err := f.svc.ProjectResources(projectID)
	if err != nil || len(resources) != 2 || resources[0].Source.OriginalPath == outside || resources[1].Source.OriginalPath == outside {
		t.Fatalf("outside file entered the project: %+v %v", resources, err)
	}
	version = mcpRunVersion(t, run)
	resumed, isErr, text := f.call("editor", "skill_runs.update", map[string]any{"run_id": id, "expected_version": version, "action": "resume"})
	if isErr {
		t.Fatalf("resume: %s", text)
	}
	version = mcpRunVersion(t, resumed["run"].(map[string]any))
	if _, err = f.svc.ImportProjectResource(ctx, projectID, outside); err != nil {
		t.Fatal(err)
	}
	out, isErr, text = call("import-approved", "assets.import", map[string]any{"path": outside})
	if isErr || out["result"].(map[string]any)["deduped"] != true {
		t.Fatalf("approved import: %s", text)
	}
	listed, isErr, text := call("list-visible", "media.list", map[string]any{})
	if isErr {
		t.Fatalf("media.list: %s", text)
	}
	encoded, _ := json.Marshal(listed["result"])
	if !strings.Contains(string(encoded), result["resource"].(map[string]any)["id"].(string)) || !strings.Contains(string(encoded), out["result"].(map[string]any)["resource"].(map[string]any)["id"].(string)) {
		t.Fatalf("frozen visible set: %s", encoded)
	}
}

func TestMCPSequenceFramesBudgetIsolationAndPreview(t *testing.T) {
	for _, name := range []string{"ffmpeg", "node"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " not installed")
		}
	}
	f, _, projectID, sequenceID := programFixture(t)
	ctx := t.Context()
	source := filepath.Join(t.TempDir(), "a.mp4")
	programClip(t, source, "red")
	view, err := f.svc.ImportProjectResource(ctx, projectID, source)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := f.svc.ProjectVisibleSet(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	created, bad, raw := f.call("editor", "skill_runs.create", map[string]any{
		"project_id": projectID, "sequence_id": sequenceID, "flow": "lightweight", "skill_id": "video-use",
		"strategy": "edit", "execution_mode": "external_mcp", "visible_set_sha": visible.SHA,
		"goal": "Inspect the composed frame", "idempotency_key": "mcp-frame-run",
		"budget": map[string]any{"max_steps": 16, "max_seconds": 300, "max_frames": 1},
	})
	if bad {
		t.Fatalf("create: %s", raw)
	}
	id := created["id"].(string)
	version := mcpRunVersion(t, created)
	call := func(key, name string, args any) (map[string]any, bool, string) {
		t.Helper()
		out, isErr, text := f.call("editor", "skill_runs.tool", map[string]any{"run_id": id, "request": map[string]any{
			"name": name, "arguments": args, "expected_version": version, "idempotency_key": key,
		}})
		if run, ok := out["run"].(map[string]any); ok {
			version = mcpRunVersion(t, run)
		}
		return out, isErr, text
	}
	current, err := f.svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, isErr, text := call("insert", "timeline.edit", map[string]any{"sequence_id": sequenceID, "command": project.Command{
		Session: current.Session, ExpectedRevision: current.Sequence.Revision, IdempotencyKey: "insert",
		Operations: []project.Operation{{Type: "clip.insert", TrackID: current.Sequence.Tracks[0].ID, ResourceID: view.Document.Resources[0].ID}},
	}}); isErr {
		t.Fatalf("insert: %s", text)
	}
	if _, isErr, text := call("other-sequence", "sequence.frames", map[string]any{"sequence_id": "seq_other", "frames": []int{0}}); !isErr {
		t.Fatalf("other sequence was accepted: %s", text)
	}
	got, _, text := f.call("editor", "skill_runs.get", map[string]any{"run_id": id})
	run := got["run"].(map[string]any)
	if run["reason"] != "authorization_revoked" || run["frames_spent"].(float64) != 0 {
		t.Fatalf("isolation: %s", text)
	}
	version = mcpRunVersion(t, run)
	resumed, isErr, text := f.call("editor", "skill_runs.update", map[string]any{"run_id": id, "expected_version": version, "action": "resume"})
	if isErr {
		t.Fatalf("resume: %s", text)
	}
	version = mcpRunVersion(t, resumed["run"].(map[string]any))
	if _, isErr, text = call("over-budget", "sequence.frames", map[string]any{"frames": []int{0, 1}}); !isErr {
		t.Fatalf("budget accepted two frames: %s", text)
	}
	got, _, text = f.call("editor", "skill_runs.get", map[string]any{"run_id": id})
	run = got["run"].(map[string]any)
	if run["reason"] != "frame_budget_exceeded" || run["frames_spent"].(float64) != 0 {
		t.Fatalf("budget accounting: %s", text)
	}
	version = mcpRunVersion(t, run)
	resumed, isErr, text = f.call("editor", "skill_runs.update", map[string]any{"run_id": id, "expected_version": version, "action": "resume"})
	if isErr {
		t.Fatalf("resume budget: %s", text)
	}
	version = mcpRunVersion(t, resumed["run"].(map[string]any))
	current, err = f.svc.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, isErr, text = call("review-early", "run.finish", map[string]any{"sequence_revision": current.Sequence.Revision, "content_version": current.ContentVersion, "review": true}); !isErr {
		t.Fatalf("review finish before inspection succeeded: %s", text)
	}
	got, _, _ = f.call("editor", "skill_runs.get", map[string]any{"run_id": id})
	if got["run"].(map[string]any)["state"] != "queued" {
		t.Fatalf("review gate changed state: %+v", got["run"])
	}
	version = mcpRunVersion(t, got["run"].(map[string]any))
	out, isErr, text := call("frame-0", "sequence.frames", map[string]any{"frames": []int{0}, "content_version": current.ContentVersion})
	if isErr {
		t.Fatalf("composed frame: %s", text)
	}
	result := out["result"].(map[string]any)
	frame := result["frames"].([]any)[0].(map[string]any)
	if frame["strategy"] != "sequence:"+current.ContentVersion || out["run"].(map[string]any)["frames_spent"].(float64) != 1 {
		t.Fatalf("frame accounting: %+v", out)
	}
	artifactID := result["artifacts"].([]any)[0].(map[string]any)["id"].(string)
	version = mcpRunVersion(t, out["run"].(map[string]any))
	if _, isErr, text = call("read-frame", "artifacts.get", map[string]any{"id": artifactID}); isErr || !strings.Contains(text, "image/png") {
		t.Fatalf("native frame: %s", text)
	}
	path, _, err := f.svc.SkillArtifactPath(ctx, "agent:editor", id, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := os.ReadFile(path)
	if err != nil || len(decoded) < 8 || string(decoded[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("mcp png: %v %d", err, len(decoded))
	}
}
