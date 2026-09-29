package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func reviewHTTPServer(t *testing.T) (*httptest.Server, *service.Service) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "review-http.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)
	if err := svc.CreateAsset(t.Context(), model.Asset{AssetID: "review-asset", Status: model.AssetStatusIngested}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(svc).Handler())
	srv.Client().Timeout = 3 * time.Second
	t.Cleanup(srv.Close)
	return srv, svc
}

func TestReviewHTTPConcurrentPatchPreservesFields(t *testing.T) {
	srv, svc := reviewHTTPServer(t)
	start := make(chan struct{})
	type result struct {
		status int
		err    error
	}
	results := make(chan result, 2)
	for _, body := range []string{`{"locked":true}`, `{"human_approved":true}`} {
		go func(body string) {
			<-start
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, srv.URL+"/api/assets/review-asset", strings.NewReader(body))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := srv.Client().Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			results <- result{status: resp.StatusCode}
		}(body)
	}
	close(start)
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil || r.status != http.StatusOK {
			t.Errorf("PATCH = %d, %v", r.status, r.err)
		}
	}
	a, err := svc.GetAsset(t.Context(), "review-asset")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Locked || !a.HumanApproved {
		t.Fatalf("concurrent HTTP PATCH lost fields: %+v", a)
	}
}

func TestReviewHTTPRejectsCrossOriginWrite(t *testing.T) {
	srv, svc := reviewHTTPServer(t)
	body := `{"task_id":"cross-origin","asset_id":"review-asset","type":"tts","agent_role":"narrator"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/api/tasks", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://untrusted.example")
	req.Header.Set("Content-Type", "text/plain")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST = %d, want 403", resp.StatusCode)
	}
	if _, err := svc.GetTask(t.Context(), "cross-origin"); err == nil {
		t.Fatal("rejected browser request created a task")
	}
}

func TestReviewHTTPJSONLimitIncludesTrailingWhitespace(t *testing.T) {
	srv, _ := reviewHTTPServer(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, srv.URL+"/api/assets/review-asset", strings.NewReader(`{}`+strings.Repeat(" ", maxBodyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized JSON tail = %d, want 413", resp.StatusCode)
	}
}

func TestReviewHTTPNullPatchIsNotAnObject(t *testing.T) {
	srv, _ := reviewHTTPServer(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, srv.URL+"/api/assets/review-asset", strings.NewReader("null"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("null PATCH = %d, want 400", resp.StatusCode)
	}
}

func TestReviewHTTPRejectsUntrustedHost(t *testing.T) {
	srv, _ := reviewHTTPServer(t)
	for _, host := range []string{"untrusted.example", "localhost.untrusted.example", "127.0.0.1.untrusted.example"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/assets", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, resp.StatusCode)
		}
	}
}
