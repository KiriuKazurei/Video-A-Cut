package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func deliveryFixture(t *testing.T) (string, *Server) {
	t.Helper()
	// The Windows sandbox permits ordinary test temp files but denies
	// EvalSymlinks on their parent. Keep this filesystem-boundary test under
	// the repository's ignored runtime directory.
	workspace, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	testParent := filepath.Join(workspace, ".run-data", "delivery-tests")
	if err := os.MkdirAll(testParent, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(testParent, "case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rel, err := filepath.Rel(testParent, root)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			_ = os.RemoveAll(root)
		}
	})
	packagePath := filepath.Join(root, "delivery-final")
	if err := os.Mkdir(packagePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(packagePath, "media"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"media/clip.mp4": []byte("mock-video-content"),
		"voice.wav":      []byte("mock-audio-content"),
		"edit.xml":       []byte("<xmeml/>"),
	} {
		if err := os.WriteFile(filepath.Join(packagePath, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"schema_version":1,"artifacts":[{"kind":"source_video","path":"media/clip.mp4"},{"kind":"voice","path":"voice.wav"},{"kind":"xmeml","path":"edit.xml"}]}`
	if err := os.WriteFile(filepath.Join(packagePath, "delivery-manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newServer(t)
	if err := srv.svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	return root, srv
}

func deliveryRequest(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := localRequest(method, path, strings.NewReader(body))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestDeliveryImportPreviewRangeAndZip(t *testing.T) {
	_, srv := deliveryFixture(t)
	imported := deliveryRequest(t, srv, http.MethodPost, "/api/deliveries/import", `{"asset_id":"demo","package_dir":"delivery-final"}`)
	if imported.Code != http.StatusCreated {
		t.Fatalf("import = %d %s", imported.Code, imported.Body.String())
	}
	listed := deliveryRequest(t, srv, http.MethodGet, "/api/assets/demo/files", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("files = %d %s", listed.Code, listed.Body.String())
	}
	var files []service.DeliveryFile
	if err := json.Unmarshal(listed.Body.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("files = %d, want manifest and three artifacts", len(files))
	}
	if strings.Contains(listed.Body.String(), "delivery-final") {
		t.Fatal("response leaked a filesystem path")
	}

	videoReq := localRequest(http.MethodGet, "/api/assets/demo/files/source_video", nil)
	videoReq.Header.Set("Range", "bytes=0-3")
	video := httptest.NewRecorder()
	srv.Handler().ServeHTTP(video, videoReq)
	if video.Code != http.StatusPartialContent || video.Body.String() != "mock" {
		t.Fatalf("range = %d %q", video.Code, video.Body.String())
	}
	if got := video.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("mime = %q", got)
	}

	xml := deliveryRequest(t, srv, http.MethodGet, "/api/assets/demo/files/xmeml", "")
	if !strings.HasPrefix(xml.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("XML must download, not render inline")
	}

	archive := deliveryRequest(t, srv, http.MethodGet, "/api/assets/demo/delivery.zip", "")
	if archive.Code != http.StatusOK {
		t.Fatalf("zip = %d %s", archive.Code, archive.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(archive.Body.Bytes()), int64(archive.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 5 {
		t.Fatalf("zip entries = %d", len(zr.File))
	}
	foundNested := false
	foundRelinkNote := false
	for _, file := range zr.File {
		if file.Name == "media/clip.mp4" {
			foundNested = true
		}
		if file.Name == "README-RELINK.txt" {
			foundRelinkNote = true
		}
		if strings.HasPrefix(file.Name, "/") || strings.Contains(file.Name, "..") {
			t.Fatalf("unsafe zip name %q", file.Name)
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, opened)
		_ = opened.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !foundNested {
		t.Fatal("ZIP did not preserve the package media path")
	}
	if !foundRelinkNote {
		t.Fatal("ZIP omitted relocation instructions for absolute XML paths")
	}
}

func TestDeliveryImportRejectsEscapeAndMissingArtifact(t *testing.T) {
	root, srv := deliveryFixture(t)
	bad := deliveryRequest(t, srv, http.MethodPost, "/api/deliveries/import", `{"asset_id":"escape","package_dir":"../"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("parent traversal = %d", bad.Code)
	}
	manifest := `{"schema_version":1,"artifacts":[{"kind":"voice","path":"missing.wav"}]}`
	if err := os.WriteFile(filepath.Join(root, "delivery-final", "delivery-manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := deliveryRequest(t, srv, http.MethodPost, "/api/deliveries/import", `{"asset_id":"missing","package_dir":"delivery-final"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing artifact = %d %s", missing.Code, missing.Body.String())
	}
	asset := deliveryRequest(t, srv, http.MethodGet, "/api/assets/missing", "")
	if asset.Code != http.StatusNotFound {
		t.Fatal("incomplete package was registered")
	}
}
