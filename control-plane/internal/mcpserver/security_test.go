package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestSecurityDesktopMCPConnectionBoundary(t *testing.T) {
	f := newLoopFixture(t, time.Minute)
	handler := New(nil, f.svc, time.Minute, Options{LocalAccess: true})
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"human:reported-only","version":"1"}}}`
	post := func(body, session string, mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "http://127.0.0.1:8787/mcp", strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:45678"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		if mutate != nil {
			mutate(req)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"nonlocal peer", func(r *http.Request) { r.RemoteAddr = "192.0.2.4:8765" }, 403},
		{"rebinding host", func(r *http.Request) { r.Host = "evil.example:8787" }, 403},
		{"browser", func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8787") }, 403},
		{"forwarded", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "192.0.2.4") }, 403},
		{"bad worker token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 40)) }, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := post(initialize, "", tc.mutate); got.Code != tc.status {
				t.Fatalf("%d: %s", got.Code, got.Body.String())
			}
		})
	}
	connected := post(initialize, "", nil)
	id := connected.Header().Get("Mcp-Session-Id")
	if connected.Code != 200 || len(id) < 32 {
		t.Fatalf("connect %d %s", connected.Code, connected.Body.String())
	}
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	if got := post(call, "", nil); got.Code != 400 {
		t.Fatal("local tools bypassed connection initialization")
	}
	if got := post(call, "unknown-session", nil); got.Code != 404 {
		t.Fatal("unknown session accepted")
	}
	listed := post(call, id, nil)
	if listed.Code != 200 {
		t.Fatalf("tools: %s", listed.Body.String())
	}
	var envelope struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Result.Tools) != 17 {
		t.Fatalf("unexpected desktop tool projection: %s", listed.Body.String())
	}
	for _, tool := range envelope.Result.Tools {
		if tool.Name == "claim_task" || tool.Name == "list_assets" || tool.Name == "skill_runs.finalize" {
			t.Fatalf("privileged worker/human tool exposed: %s", tool.Name)
		}
	}
	changed := strings.Replace(initialize, `human:reported-only`, `another-agent`, 1)
	if got := post(changed, id, nil); got.Code != 400 {
		t.Fatal("changed declared identity accepted in same session")
	}
	req := httptest.NewRequest("DELETE", "http://127.0.0.1:8787/mcp", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	req.Header.Set("Mcp-Session-Id", id)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("disconnect: %d %s", w.Code, w.Body.String())
	}
	if got := post(call, id, nil); got.Code != 404 {
		t.Fatal("closed session reused")
	}
}

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
