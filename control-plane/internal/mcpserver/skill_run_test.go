package mcpserver

import (
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
