package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameOriginWebHandlerServesBuildAndKeepsAPI(t *testing.T) {
	workspace, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(workspace, ".run-data", "web-tests")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(parent, "case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rel, err := filepath.Rel(parent, root)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			_ = os.RemoveAll(root)
		}
	})
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>control station</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("console.log('ok')"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("api-response")) })
	handler, err := sameOriginWebHandler(root, api)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		path   string
		status int
		body   string
	}{
		{"/", 200, "control station"},
		{"/assets/app.js", 200, "console.log"},
		{"/api/assets", 200, "api-response"},
		{"/assets/", 404, ""},
		{"/missing", 404, ""},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, check.path, nil))
		if rec.Code != check.status || (check.body != "" && !strings.Contains(rec.Body.String(), check.body)) {
			t.Errorf("%s = %d %q", check.path, rec.Code, rec.Body.String())
		}
	}
}
