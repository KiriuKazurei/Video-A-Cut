package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderDiagnosticsRESTAndCredentialBoundary(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer temporary-key" {
			t.Error("missing transient key")
		}
		if r.Method == "GET" {
			w.Write([]byte(`{"data":[{"id":"model-one"}]}`))
		} else {
			w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
		}
	}))
	defer upstream.Close()
	srv := newServer(t)
	provider := map[string]any{"adapter": "narration", "api_format": "openai", "endpoint": upstream.URL, "token_env": "VAC_DIAGNOSTIC_UNSET", "model": "model-one"}
	for _, operation := range []string{"models", "test"} {
		body, _ := json.Marshal(map[string]any{"provider": provider, "api_key": "temporary-key"})
		r := postJSON(t, srv, "/api/providers/"+operation, string(body))
		if r.Code != 200 || !strings.Contains(r.Body.String(), `"ok":true`) || strings.Contains(r.Body.String(), "temporary-key") || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("bad diagnostic %d %s", r.Code, r.Body)
		}
	}
	if calls != 2 {
		t.Fatal("diagnostics skipped actual requests")
	}
	// Diagnostics neither create profiles nor silently grant source-media consent.
	r := httptest.NewRecorder()
	srv.Handler().ServeHTTP(r, localRequest("GET", "/api/processing-profiles", nil))
	if r.Body.String() != "[]" {
		t.Fatalf("profile persisted %s", r.Body)
	}
	body, _ := json.Marshal(map[string]any{"provider": provider})
	r = postJSON(t, srv, "/api/providers/test", string(body))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "missing_credentials") {
		t.Fatalf("missing key %s", r.Body)
	}
	provider["api_format"] = "other"
	body, _ = json.Marshal(map[string]any{"provider": provider, "api_key": "temporary-key"})
	if r := postJSON(t, srv, "/api/providers/models", string(body)); r.Code != 400 {
		t.Fatal("bad format accepted")
	}
	if calls != 2 {
		t.Fatal("invalid diagnostics made network requests")
	}
}
