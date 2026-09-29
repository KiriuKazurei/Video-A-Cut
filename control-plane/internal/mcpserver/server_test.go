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
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func TestLoadRejectsInvalidCredentialFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	for _, document := range []string{
		`{"agents":[]}`,
		`{"agents":[{"agent_id":"a","role":"narrator","token_sha256":"short"}]}`,
		`{"agents":[{"agent_id":"a","role":"narrator","token_sha256":"` + strings.Repeat("a", 64) + `"},{"agent_id":"b","role":"narrator","token_sha256":"` + strings.Repeat("a", 64) + `"}]}`,
	} {
		if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, nil, 0); err == nil {
			t.Fatalf("accepted invalid credentials: %s", document)
		}
	}
}

func TestAgentBoundaryAndVisibleAssets(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)
	for _, a := range []model.Asset{
		{AssetID: "visible", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}},
		{AssetID: "locked", Status: model.AssetStatusIngested, AgentVisible: true, Locked: true, AllowedAgents: []string{"narrator"}},
		{AssetID: "foreign", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"exporter"}},
	} {
		if err := svc.CreateAsset(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	token := strings.Repeat("n", 40)
	hash := sha256.Sum256([]byte(token))
	server := httptest.NewServer(New([]Agent{{ID: "narrator-1", Role: "narrator", TokenSHA256: hex.EncodeToString(hash[:])}}, svc, 0))
	defer server.Close()
	post := func(name, body, bearer, origin string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", name)
		if name == "tools/call" {
			var request struct {
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			_ = json.Unmarshal([]byte(body), &request)
			req.Header.Set("Mcp-Name", request.Params.Name)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + meta + `}}`
	if status, _ := post("tools/list", list, "", ""); status != 401 {
		t.Fatalf("anonymous status = %d", status)
	}
	if status, _ := post("tools/list", list, strings.Repeat("x", 40), ""); status != 401 {
		t.Fatalf("invalid token status = %d", status)
	}
	if status, _ := post("tools/list", list, token, "http://evil.example"); status != 403 {
		t.Fatalf("browser origin status = %d", status)
	}
	status, result := post("tools/list", list, token, "")
	if status != 200 || !strings.Contains(result, "list_assets") || !strings.Contains(result, "get_asset") || !strings.Contains(result, "submit_result") || strings.Contains(result, "set_agent_visible") {
		t.Fatalf("tools/list: %d %s", status, result)
	}
	call := func(id string) (int, string) {
		body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_asset","arguments":{"asset_id":"` + id + `"},` + meta + `}}`
		return post("tools/call", body, token, "")
	}
	if status, result := call("visible"); status != 200 || !strings.Contains(result, `"asset_id":"visible"`) {
		t.Fatalf("visible asset: %d %s", status, result)
	}
	if status, result := call("locked"); status != 200 || strings.Contains(result, `"asset_id":"locked"`) || !strings.Contains(result, `"isError":true`) {
		t.Fatalf("locked asset: %d %s", status, result)
	}
	if status, result := call("foreign"); status != 200 || strings.Contains(result, `"asset_id":"foreign"`) || !strings.Contains(result, `"isError":true`) {
		t.Fatalf("foreign asset: %d %s", status, result)
	}
}

func TestSubmitUsesTokenIdentity(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)
	ctx := context.Background()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateTask(ctx, model.Task{TaskID: "task", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimTask(ctx, "owner", "narrator", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ownerToken, otherToken := strings.Repeat("o", 40), strings.Repeat("p", 40)
	ownerHash, otherHash := sha256.Sum256([]byte(ownerToken)), sha256.Sum256([]byte(otherToken))
	server := httptest.NewServer(New([]Agent{
		{ID: "owner", Role: "narrator", TokenSHA256: hex.EncodeToString(ownerHash[:])},
		{ID: "other", Role: "narrator", TokenSHA256: hex.EncodeToString(otherHash[:])},
	}, svc, 0))
	defer server.Close()
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"submit_result","arguments":{"task_id":"task","artifacts":{"voice":"voice.wav"}},` + meta + `}}`
	call := func(token string) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "tools/call")
		req.Header.Set("Mcp-Name", "submit_result")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status %d: %s", resp.StatusCode, buf.String())
		}
		return buf.String()
	}
	if result := call(otherToken); !strings.Contains(result, `"isError":true`) {
		t.Fatalf("other agent submit was accepted: %s", result)
	}
	if task, err := svc.GetTask(ctx, "task"); err != nil || task.Status != model.TaskStatusClaimed {
		t.Fatalf("other agent changed task: %+v %v", task, err)
	}
	if result := call(ownerToken); strings.Contains(result, `"isError":true`) || !strings.Contains(result, `"submitted":true`) {
		t.Fatalf("owner submit failed: %s", result)
	}
	if task, err := svc.GetTask(ctx, "task"); err != nil || task.Status != model.TaskStatusSucceeded {
		t.Fatalf("owner result missing: %+v %v", task, err)
	}
}
