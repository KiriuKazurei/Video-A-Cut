package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/config"
)

func TestMCPMountIsOptIn(t *testing.T) {
	request := func(handler http.Handler, token string) *httptest.ResponseRecorder {
		t.Helper()
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "tools/list")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	cfg := config.Default()
	cfg.HttpAddr = "127.0.0.1:0"
	appWithoutMCP, err := assemble(filepath.Join(t.TempDir(), "without.db"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appWithoutMCP.Close() })
	if got := request(appWithoutMCP.httpSrv.Handler, ""); got.Code != http.StatusNotFound {
		t.Fatalf("disabled /mcp status = %d", got.Code)
	}

	token := strings.Repeat("a", 40)
	hash := sha256.Sum256([]byte(token))
	path := filepath.Join(t.TempDir(), "agents.json")
	doc := `{"agents":[{"agent_id":"agent-1","role":"narrator","token_sha256":"` + hex.EncodeToString(hash[:]) + `"}]}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.MCPAgentsFile = path
	appWithMCP, err := assemble(filepath.Join(t.TempDir(), "with.db"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appWithMCP.Close() })
	if got := request(appWithMCP.httpSrv.Handler, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized /mcp status = %d", got.Code)
	}
	if got := request(appWithMCP.httpSrv.Handler, token); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "list_assets") {
		t.Fatalf("authorized /mcp status = %d body = %s", got.Code, got.Body.String())
	}
}
