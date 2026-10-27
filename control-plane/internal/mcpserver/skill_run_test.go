package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func TestProjectMCPResourcesFollowOpenProject(t *testing.T) {
	f := newLoopFixtureOptions(t, time.Minute, Options{LocalAccess: true})
	ctx := t.Context()
	root := t.TempDir()
	if err := f.svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ConfigureProjectWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.svc.CloseProjectWorkspace)
	first, err := f.svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "a.avp"), Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "notes.md")
	if err = os.WriteFile(source, []byte("only-project-A"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err = f.svc.ImportProjectResource(ctx, first.Document.Project.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	client := f.sdkClient(ctx, "")
	read := func(projectID string) *mcp.CallToolResult {
		r, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "resources.read", Arguments: map[string]any{"project_id": projectID, "resource_id": first.Document.Resources[0].ID}})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := read(first.Document.Project.ID)
	raw, _ := json.Marshal(r)
	if r.IsError || !bytes.Contains(raw, []byte("only-project-A")) {
		t.Fatal("current resource unreadable", string(raw))
	}
	second, err := f.svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "b.avp"), Name: "B", Decision: "discard"})
	if err != nil {
		t.Fatal(err)
	}
	if !read(first.Document.Project.ID).IsError {
		t.Fatal("old project read through MCP")
	}
	listed, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "projects.list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(listed)
	if bytes.Contains(raw, []byte(first.Document.Project.ID)) || !bytes.Contains(raw, []byte(second.Document.Project.ID)) {
		t.Fatal("MCP discovered closed project", string(raw))
	}
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

// The SDK owns discovery, headers, protocol negotiation and serialization here;
// unlike loopFixture.call this exercises an actual external MCP client.
func (f *loopFixture) sdkClient(ctx context.Context, agent string) *mcp.ClientSession {
	f.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "vac-integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   f.server.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{f.tokens[agent], f.server.Client().Transport}},
	}, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestSkillMCPSDKMediaEditingLifecycle(t *testing.T) {
	for _, mode := range []string{"worker-bearer", "desktop-mcp"} {
		t.Run(mode, func(t *testing.T) { testSkillMCPSDKMediaEditingLifecycle(t, mode == "desktop-mcp") })
	}
}

func testSkillMCPSDKMediaEditingLifecycle(t *testing.T, local bool) {
	for _, executable := range []string{"ffmpeg", "ffprobe", "python"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skip(executable + " not installed")
		}
	}
	f := newLoopFixtureOptions(t, time.Minute, Options{LocalAccess: local}, Agent{ID: "editor", Role: "narrator"})
	ctx := t.Context()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	media, err := os.ReadFile(filepath.Join(repo, "workers/node/timeline-cli/fixtures/media/clip_01.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "a.mp4"), media, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "edl.json"), []byte(`{"video":[{"src":"a.mp4","in":0,"out":2}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ConfigureTTS("python", filepath.Join(repo, "workers/python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.svc.CloseTTS)
	f.seed([]model.Asset{{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{"edl": "edl.json"}}}, nil)
	if err = f.svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.SaveProject(ctx, "human:test", "", service.ProjectRequest{Name: "SDK editing", Flow: "lightweight", Assets: []string{"clip"}, Visible: []string{"clip"}, AcceptExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.ProjectVisibleSet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.ConfirmProjectVisible(ctx, "human:test", p.ID, view.SHA, p.Version); err != nil {
		t.Fatal(err)
	}
	agent := "editor"
	if local {
		agent = ""
	}
	session := f.sdkClient(ctx, agent)
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]bool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = true
	}
	for _, name := range []string{"skills.get", "skills.read_resource", "projects.list", "projects.visible_set.get", "skill_runs.claim", "skill_runs.create", "skill_runs.get", "skill_runs.progress", "skill_runs.tool"} {
		if !tools[name] {
			t.Fatalf("missing discovered tool %s", name)
		}
	}
	for _, name := range []string{"skill_runs.finalize", "skill_runs.undo", "agent_operations.list"} {
		if tools[name] {
			t.Fatalf("human tool exposed: %s", name)
		}
	}
	call := func(name string, args any, target any) *mcp.CallToolResult {
		t.Helper()
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || result.IsError {
			t.Fatalf("%s: %v %+v", name, e, result)
		}
		if target != nil {
			raw, e := json.Marshal(result.StructuredContent)
			if e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(raw, target); e != nil {
				t.Fatal(e)
			}
		}
		return result
	}
	var pkg skill.Package
	call("skills.get", map[string]any{"id": "video-use"}, &pkg)
	var resource map[string]string
	call("skills.read_resource", map[string]any{"id": pkg.ID, "resource": "upstream/SKILL.md", "content_sha256": pkg.SHA}, &resource)
	if resource["content"] == "" {
		t.Fatal("missing upstream instructions")
	}
	call("skills.read_resource", map[string]any{"id": pkg.ID, "resource": "vac/tool-contracts.md", "content_sha256": pkg.SHA}, &resource)
	if resource["content"] != skill.ToolContracts {
		t.Fatal("wrong frozen tool contract")
	}
	var run skill.Run
	call("skill_runs.create", skill.RunRequest{ProjectID: p.ID, SkillID: "video-use", Mode: "external_mcp", VisibleSHA: view.SHA, Goal: "Keep two short moments", IdempotencyKey: "sdk-create"}, &run)
	call("skill_runs.progress", map[string]any{"run_id": run.ID, "expected_version": run.Version, "phase": "generating", "output_chars": 128}, &run)
	if run.Progress == nil || run.Progress.Phase != "generating" || run.Progress.OutputChars != 128 || run.Steps != 0 {
		t.Fatalf("progress not returned or consumed a step: %+v", run)
	}
	if local {
		other := f.sdkClient(ctx, "") // same declared name, separate MCP session
		denied, e := other.CallTool(ctx, &mcp.CallToolParams{Name: "skill_runs.get", Arguments: map[string]any{"run_id": run.ID}})
		if e != nil || !denied.IsError {
			t.Fatalf("another connection accessed run: %+v %v", denied, e)
		}
		denied, e = other.CallTool(ctx, &mcp.CallToolParams{Name: "skill_runs.progress", Arguments: map[string]any{"run_id": run.ID, "expected_version": run.Version, "phase": "generating"}})
		if e != nil || !denied.IsError {
			t.Fatalf("another connection renewed the lease: %+v %v", denied, e)
		}
	}
	step := 0
	tool := func(name string, args any) (json.RawMessage, *mcp.CallToolResult) {
		t.Helper()
		step++
		var out struct {
			Run    skill.Run       `json:"run"`
			Result json.RawMessage `json:"result"`
		}
		result := call("skill_runs.tool", map[string]any{"run_id": run.ID, "request": map[string]any{"name": name, "arguments": args, "expected_version": run.Version, "idempotency_key": fmt.Sprint("sdk-", step)}}, &out)
		run = out.Run
		return out.Result, result
	}
	tool("media.list", map[string]any{})
	tool("media.get_frames", map[string]any{})
	if len(run.Frames) == 0 {
		t.Fatal("no extracted frames")
	}
	frame, result := tool("artifacts.get", map[string]any{"id": run.Frames[0].ID})
	var imagePayload struct {
		Artifact skill.Artifact `json:"artifact"`
	}
	if err := json.Unmarshal(frame, &imagePayload); err != nil {
		t.Fatal(err)
	}
	images := 0
	var decoded []byte
	for _, content := range result.Content {
		if img, ok := content.(*mcp.ImageContent); ok {
			images++
			decoded = img.Data
			if img.MIMEType != "image/jpeg" {
				t.Fatalf("image MIME: %s", img.MIMEType)
			}
		}
		if txt, ok := content.(*mcp.TextContent); ok && strings.Contains(txt.Text, `"base64"`) {
			t.Fatal("frame duplicated into model text")
		}
	}
	if images != 1 || int64(len(decoded)) != imagePayload.Artifact.Size || len(decoded) < 100 {
		t.Fatalf("invalid native frame: images=%d bytes=%d", images, len(decoded))
	}
	t.Logf("MCP frame delivery: %d decoded bytes, %d native image content blocks", len(decoded), images)
	tool("summaries.submit", map[string]any{
		"model": "deterministic-client-fixture", "narrative": "Shared fixture coverage", "beats": []string{"Opening"},
		"timeline": []any{map[string]any{"media_id": view.Media[0].ID, "start": 0, "end": .5, "description": "Fixture evidence", "confidence": "low", "frame_ids": []string{run.Frames[0].ID}}},
	})
	if len(run.Summary) == 0 {
		t.Fatal("MCP summary not persisted")
	}
	plan := skill.Plan{Body: "Opening\n\nEnding", Segments: []skill.Segment{{MediaID: view.Media[0].ID, Start: 0, End: .5}, {MediaID: view.Media[0].ID, Start: .5, End: 1}}}
	tool("scripts.propose", plan)
	if run.State != "waiting_human" || run.Plan.FinalizedBy != "" {
		t.Fatal("draft bypassed human gate")
	}
	if _, err = f.svc.UpdateSkillRun(ctx, "agent:editor", run.ID, "finalize", run.Version); err == nil {
		t.Fatal("Agent finalized own plan")
	}
	finalize := func() {
		t.Helper()
		run, err = f.svc.UpdateSkillRun(ctx, "human:test", run.ID, "finalize", run.Version)
		if err != nil {
			t.Fatal(err)
		}
	}
	finish := func() skill.Artifact {
		t.Helper()
		tool("renders.create", map[string]any{})
		var mp4 skill.Artifact
		premiere := false
		for _, a := range run.Artifacts {
			if a.Kind == "mp4" && a.PlanVersion == run.Plan.Version {
				mp4 = a
			}
			premiere = premiere || strings.Contains(a.Kind, "premiere")
		}
		if mp4.ID == "" || !premiere {
			t.Fatalf("missing delivery: %+v", run.Artifacts)
		}
		path, _, e := f.svc.SkillArtifactPath(ctx, "human:test", run.ID, mp4.ID)
		if e != nil {
			t.Fatal(e)
		}
		probe, e := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", path).CombinedOutput()
		if e != nil {
			t.Fatalf("invalid MP4: %v %s", e, probe)
		}
		var info struct {
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
		}
		if e = json.Unmarshal(probe, &info); e != nil {
			t.Fatal(e)
		}
		want := "1.000000"
		if run.Plan.Version == 2 {
			want = "0.800000"
		}
		if info.Format.Duration != want {
			t.Fatalf("render duration %s, want %s", info.Format.Duration, want)
		}
		t.Logf("Rendered plan %d: %s", run.Plan.Version, probe)
		tool("renders.inspect", map[string]any{})
		ids := append([]string{}, run.Inspection.FrameIDs...)
		for _, id := range ids {
			tool("artifacts.get", map[string]any{"id": id})
		}
		tool("renders.review", map[string]any{"plan_version": run.Plan.Version, "frame_ids": ids, "verdict": "passed", "issues": []string{}})
		tool("run.finish", map[string]any{})
		if run.State != "succeeded" {
			t.Fatalf("not completed: %s", run.State)
		}
		return mp4
	}
	finalize()
	first := finish()
	index := 0
	run, err = f.svc.RequestSkillEdit(ctx, "human:test", run.ID, service.SkillEditRequest{ExpectedVersion: run.Version, PlanVersion: run.Plan.Version, Prompt: "Shorten opening only", SegmentIndex: &index, ArtifactID: first.ID, ArtifactSHA: first.SHA, IdempotencyKey: "sdk-edit"})
	if err != nil {
		t.Fatal(err)
	}
	var context struct {
		Run skill.Run `json:"run"`
	}
	call("skill_runs.get", map[string]any{"run_id": run.ID}, &context)
	if context.Run.EditRequest == nil || context.Run.EditRequest.Windows[0].TimelineEnd != .5 || context.Run.EditRequest.Windows[0].SourceEnd != .5 {
		t.Fatal("MCP lost edit prompt/windows")
	}
	plan.Segments[0].End = .3
	tool("scripts.propose", plan)
	if run.Plan.FinalizedBy != "" || run.Plan.Segments[1] != plan.Segments[1] {
		t.Fatal("local edit changed outside target or preserved approval")
	}
	finalize()
	second := finish()
	if first.SHA == second.SHA {
		t.Fatal("local edit did not produce new video")
	}
	if _, err = f.svc.AgentOperations(ctx, "agent:editor", run.ID, 0, 100); err == nil {
		t.Fatal("Agent can read journal")
	}
	if _, err = f.svc.VerifyAgentOperations(ctx, "human:test"); err != nil {
		t.Fatal(err)
	}
	if local {
		logs, e := f.svc.AgentOperations(ctx, "human:test", run.ID, 0, 100)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(logs)
		if !bytes.Contains(raw, []byte(`"name":"vac-integration"`)) || !bytes.Contains(raw, []byte(`"identity_kind":"client_reported"`)) || !bytes.Contains(raw, []byte(`"source":"Go-http-client/1.1"`)) {
			t.Fatalf("missing reported Agent origin/name: %s", raw)
		}
	}
	t.Logf("Official SDK discovery, Skill distribution, two MP4/Premiere deliveries, scoped edit, human gates and journal verified; no provider configured")
}

func TestSkillMCPExternalAgentCanRunWithoutVACModelKey(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "editor", Role: "narrator"}, Agent{ID: "other", Role: "narrator"})
	ctx := t.Context()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.mp4"), []byte("media metadata fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "edl.json"), []byte(`{"video":[{"src":"a.mp4","in":0,"out":1}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	f.seed([]model.Asset{{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{"edl": "edl.json"}}}, nil)
	if err := f.svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.SaveProject(ctx, "human:test", "", service.ProjectRequest{Name: "MCP", Flow: "lightweight", Assets: []string{"clip"}, Visible: []string{"clip"}, AcceptExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.ProjectVisibleSet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.ConfirmProjectVisible(ctx, "human:test", p.ID, view.SHA, p.Version); err != nil {
		t.Fatal(err)
	}
	created, bad, raw := f.call("editor", "skill_runs.create", map[string]any{"project_id": p.ID, "skill_id": "video-use", "execution_mode": "external_mcp", "visible_set_sha": view.SHA, "goal": "Inspect authorized media", "idempotency_key": "mcp-create"})
	if bad {
		t.Fatalf("external run: %s", raw)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("missing run: %s", raw)
	}
	_, bad, raw = f.call("editor", "skill_runs.tool", map[string]any{"run_id": id, "request": map[string]any{"name": "media.list", "arguments": map[string]any{}, "expected_version": created["version"], "idempotency_key": "mcp-list"}})
	if bad {
		t.Fatalf("media tool: %s", raw)
	}
	_, bad, _ = f.call("other", "skill_runs.get", map[string]any{"run_id": id})
	if !bad {
		t.Fatal("another Agent read this run")
	}
	for _, name := range []string{"skill_runs.steps", "agent_operations.list", "skill_runs.undo"} {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{"run_id": id}, "_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}})
		req, _ := http.NewRequest("POST", f.server.URL, strings.NewReader(string(body)))
		for k, v := range map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": name, "Authorization": "Bearer " + f.tokens["editor"]} {
			req.Header.Set(k, v)
		}
		response, e := f.server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || response.StatusCode != 400 || !strings.Contains(string(raw), "unknown tool") {
			t.Fatalf("human-only tool exposed: %s %s %v", name, raw, e)
		}
	}
	rows, err := f.svc.AgentOperations(ctx, "human:test", id, 0, 100)
	if err != nil || len(rows) < 4 {
		t.Fatalf("MCP attempts not journaled: %d %v", len(rows), err)
	}
	if _, err = f.svc.VerifyAgentOperations(ctx, "human:test"); err != nil {
		t.Fatal(err)
	}
}
