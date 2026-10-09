package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopOriginGate(t *testing.T) {
	handler := localRequestGateWithOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), "http://tauri.localhost")
	for _, c := range []struct {
		origin, host, method, body, contentType string
		want                                    int
	}{
		{"http://tauri.localhost", "127.0.0.1:1234", "GET", "", "", 200},
		{"http://evil.test", "127.0.0.1:1234", "GET", "", "", 403},
		{"http://tauri.localhost", "evil.test", "GET", "", "", 403},
		{"http://tauri.localhost", "127.0.0.1:1234", "POST", "{}", "text/plain", 415},
		{"http://tauri.localhost", "127.0.0.1:1234", "POST", "{}", "application/json", 200},
	} {
		r := httptest.NewRequest(c.method, "http://"+c.host+"/api/assets", strings.NewReader(c.body))
		r.Header.Set("Origin", c.origin)
		r.Header.Set("Content-Type", c.contentType)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Fatalf("%+v: %d", c, w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Fatal("wildcard origin")
		}
	}
	r := httptest.NewRequest("OPTIONS", "http://127.0.0.1:1234/api/assets", nil)
	r.Header.Set("Origin", "http://tauri.localhost")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	r.Header.Set("Access-Control-Request-Headers", "authorization")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
