package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// Phase 3 task 4: the security regression matrix for the agent surface.

func TestSecurityGovernanceChangeTakesEffectImmediately(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "n1", Role: "narrator"})
	root := t.TempDir()
	if err := f.svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "p"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "p", "edl.json"), []byte(`{"timeline":{"fps":30}}`), 0o644)
	f.seed([]model.Asset{{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
		AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{"edl": "p/edl.json"}}}, nil)

	if out, isErr, raw := f.call("n1", "get_asset_edl", map[string]string{"asset_id": "clip"}); isErr || !strings.Contains(raw, `"fps":30`) || out["asset_id"] != "clip" {
		t.Fatalf("visible edl: %s", raw)
	}
	ctx := context.Background()
	for name, patch := range map[string]service.GovernancePatch{
		"lock":   {Locked: ptr(true)},
		"hide":   {Locked: ptr(false), AgentVisible: ptr(false)},
		"revoke": {AgentVisible: ptr(true), AllowedAgents: &[]string{"exporter"}},
	} {
		if _, err := f.svc.PatchAssetGovernance(ctx, "human:test", "clip", patch); err != nil {
			t.Fatal(err)
		}
		for _, tool := range []string{"get_asset", "get_asset_edl"} {
			if _, isErr, raw := f.call("n1", tool, map[string]string{"asset_id": "clip"}); !isErr || strings.Contains(raw, "fps") {
				t.Fatalf("%s after %s: %s", tool, name, raw)
			}
		}
		if out, _, raw := f.call("n1", "list_assets", struct{}{}); len(out["assets"].([]any)) != 0 {
			t.Fatalf("list after %s: %s", name, raw)
		}
	}
	// Unknown asset answers exactly like an invisible one.
	_, _, unknown := f.call("n1", "get_asset_edl", map[string]string{"asset_id": "nope"})
	_, _, hidden := f.call("n1", "get_asset_edl", map[string]string{"asset_id": "clip"})
	if strings.ReplaceAll(unknown, "nope", "") != strings.ReplaceAll(hidden, "clip", "") {
		t.Fatalf("unknown vs hidden differ:\n%s\n%s", unknown, hidden)
	}
}

func ptr[T any](v T) *T { return &v }

func TestSecurityTokenIsolationAndTransport(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "n1", Role: "narrator"})
	post := func(header map[string]string, body string) int {
		req, _ := http.NewRequest(http.MethodPost, f.server.URL, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "tools/list")
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	good := f.tokens["n1"]
	h := sha256.Sum256([]byte(good))
	for name, c := range map[string]struct {
		header map[string]string
		want   int
	}{
		"no auth":        {map[string]string{}, 401},
		"basic scheme":   {map[string]string{"Authorization": "Basic " + good}, 401},
		"lowercase":      {map[string]string{"Authorization": "bearer " + good}, 401},
		"hash as token":  {map[string]string{"Authorization": "Bearer " + hex.EncodeToString(h[:])}, 401},
		"token prefix":   {map[string]string{"Authorization": "Bearer " + good[:len(good)-1]}, 401},
		"token suffix":   {map[string]string{"Authorization": "Bearer " + good + "x"}, 401},
		"browser origin": {map[string]string{"Authorization": "Bearer " + good, "Origin": "http://127.0.0.1:5173"}, 403},
		"valid":          {map[string]string{"Authorization": "Bearer " + good}, 200},
	} {
		if got := post(c.header, list); got != c.want {
			t.Errorf("%s: %d want %d", name, got, c.want)
		}
	}
	// Oversized body is refused before reaching the SDK.
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"` + strings.Repeat("x", maxRequestBytes) + `"}}`
	if got := post(map[string]string{"Authorization": "Bearer " + good}, big); got == 200 {
		t.Errorf("oversized body accepted")
	}
	req, _ := http.NewRequest(http.MethodGet, f.server.URL, bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer "+good)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status %d", resp.StatusCode)
	}
}

func TestSecurityNoGovernanceOrFileToolsExposed(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "n1", Role: "narrator"})
	forbidden := []string{"set_agent_visible", "lock", "approve", "delete", "patch_asset", "import", "reopen", "read_file", "download", "create_task"}
	req, _ := http.NewRequest(http.MethodPost, f.server.URL, strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/list")
	req.Header.Set("Authorization", "Bearer "+f.tokens["n1"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	resp.Body.Close()
	for _, name := range forbidden {
		if strings.Contains(buf.String(), `"name":"`+name) {
			t.Errorf("governance/file tool %q exposed", name)
		}
	}
	// Unknown tool names are rejected by the SDK (JSON-RPC -32602), never
	// routed to service.
	unknown, _ := http.NewRequest(http.MethodPost, f.server.URL, strings.NewReader(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_agent_visible","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
	for k, v := range req.Header {
		unknown.Header[k] = v
	}
	unknown.Header.Set("Mcp-Method", "tools/call")
	unknown.Header.Set("Mcp-Name", "set_agent_visible")
	resp, err = http.DefaultClient.Do(unknown)
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	_, _ = buf.ReadFrom(resp.Body)
	resp.Body.Close()
	if !strings.Contains(buf.String(), "unknown tool") {
		t.Errorf("unknown tool not rejected: %d %s", resp.StatusCode, buf.String())
	}
}
