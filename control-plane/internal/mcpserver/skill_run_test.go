package mcpserver

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
}
