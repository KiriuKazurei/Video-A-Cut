package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func TestSystemDiagnosticsNeverCallConfiguredModelsAndAggregateWorkers(t *testing.T) {
	srv := newServer(t)
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer remote.Close()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DIAG_PRIVATE_TOKEN", "private-credential")
	profile := preparation.Profile{SchemaVersion: 1, ProfileID: "private-profile", Revision: 1, Name: "private customer", ContentMode: "configured", Vision: preparation.Provider{Adapter: "vision", Endpoint: remote.URL, Model: "private-model", TokenEnv: "DIAG_PRIVATE_TOKEN"}, Narration: preparation.Provider{Adapter: "narration", Endpoint: remote.URL, Model: "private-model", TokenEnv: "DIAG_PRIVATE_TOKEN"}, Sampling: preparation.Sampling{MaxFrames: 1, MaxBytes: 1024, TimeoutSeconds: 1}, TTSVoice: "private voice", ExportTarget: "premiere"}
	if _, err := srv.svc.SaveProcessingProfile(t.Context(), "human:test", "diagnostic-profile", 0, profile); err != nil {
		t.Fatal(err)
	}
	if err := srv.svc.Heartbeat(t.Context(), "private-worker", "recognizer", "", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	res := get(t, srv, "/api/system/diagnostics")
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	var report struct {
		Checks []struct{ ID, Status string } `json:"checks"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, c := range report.Checks {
		states[c.ID] = c.Status
	}
	if states["worker.recognizer"] != "passed" || states["worker.exporter"] != "warning" || states["models.configuration"] != "not_checked" {
		t.Fatal(states)
	}
	for _, private := range []string{"private-credential", "private-worker", "private-profile", "private customer", "private-model", remote.URL} {
		if strings.Contains(res.Body.String(), private) {
			t.Fatalf("private value in diagnostic report: %s", private)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("diagnostics called an external model")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := localRequest("GET", "/api/system/diagnostics", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestTimeout {
		t.Fatal("cancelled diagnostic returned success", rec.Code)
	}
}

func TestSystemDiagnosticsInspectLocalToolVersions(t *testing.T) {
	srv := newServer(t)
	rec := get(t, srv, "/api/system/diagnostics/report")
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var report service.DiagnosticReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Checks {
		if strings.HasPrefix(check.ID, "dependency.") && check.Status == "passed" && check.Version == "" {
			t.Fatal("passed without observed tool version", check)
		}
		if strings.Contains(check.Message, ":\\") || strings.Contains(check.Message, "http://") {
			t.Fatal("raw path or URL in report", check)
		}
	}
	t.Logf("actual local diagnostic: %s", rec.Body.String())
}

func TestSystemDiagnosticsAreRedactedAndReadOnly(t *testing.T) {
	srv := newServer(t)
	privatePath := t.TempDir()
	t.Setenv("PATH", privatePath)
	t.Setenv("VAC_TTS_TOKEN", "diagnostic-secret-must-not-leak")
	before, err := srv.svc.RuntimeActivity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/system/diagnostics", "/api/system/diagnostics/report"} {
		rec := get(t, srv, path)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		var report struct {
			Schema   int  `json:"schema_version"`
			Redacted bool `json:"redacted"`
			Checks   []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"checks"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &report) != nil || report.Schema != 1 || !report.Redacted || len(report.Checks) < 8 {
			t.Fatal(rec.Body.String())
		}
		states := map[string]string{}
		for _, check := range report.Checks {
			states[check.ID] = check.Status
		}
		if states["database"] != "passed" || states["dependency.node"] != "failed" || states["dependency.python"] != "failed" {
			t.Fatal(states)
		}
		if strings.Contains(rec.Body.String(), privatePath) || strings.Contains(rec.Body.String(), "diagnostic-secret-must-not-leak") {
			t.Fatal("diagnostics leaked private values")
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("diagnostic snapshot must not be cached")
		}
		if strings.HasSuffix(path, "/report") && !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
			t.Fatal("report is not downloadable")
		}
	}
	after, err := srv.svc.RuntimeActivity(t.Context())
	if err != nil || after != before {
		t.Fatal("diagnostics changed active work", after, err)
	}
	audit, err := srv.svc.ListAudit(t.Context(), 100)
	if err != nil || len(audit) != 0 {
		t.Fatal("diagnostics wrote audit/business state", err)
	}
}

type flushErrorWriter struct {
	header http.Header
	status int
	body   []byte
	err    error
}

func (w *flushErrorWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *flushErrorWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *flushErrorWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body = append(w.body, p...)
	return len(p), nil
}

func (w *flushErrorWriter) FlushError() error { return w.err }

// newServer returns a Server wired to a real Service over a throwaway
// database.
//
// The service is real rather than nil so the router is exercised against the
// same object graph main.go assembles: a nil service would compile and pass
// every test here until the first real handler dereferenced it, which is the
// one failure a skeleton must not hide.
func newServer(t *testing.T) *Server {
	t.Setenv("VAC_HUMAN_ACCESS_TOKEN", "vac-human-test-credential-00000000000001")
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(service.New(st))
}

// do sends one request through the router and returns the recorded response.
func do(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := localRequest(method, path, nil)
	req.Header.Set("X-VAC-Human-Token", "vac-human-test-credential-00000000000001")
	newServer(t).Handler().ServeHTTP(rec, req)
	return rec
}

func localRequest(method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	req.Host = "127.0.0.1"
	return req
}

// envelope mirrors the JSON error body every handler writes, so a test fails
// loudly when a response is not that shape instead of reading zero values.
func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var env apiErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not the JSON error envelope: %v (%q)", err, rec.Body.String())
	}
	return env.Error
}

// TestHandlerServesEveryDocumentedRoute pins the URL surface of docs §9.1:
// each of the seven endpoints must be reachable, and must answer through the
// JSON writer rather than ServeMux's plain-text default, so a client sees one
// error shape no matter which endpoint it called.
//
// want is the status the endpoint is expected to answer with today. An
// implemented endpoint answers 200 (or 404 for a well-formed unknown id); a
// PATCH with no body at all is a client bug and answers 400; one still
// awaiting its handler answers 501. The point of the table is that every URL
// is registered and every answer is JSON — a route that regressed to
// ServeMux's plain text would fail on the body shape below, not on this
// number.
func TestHandlerServesEveryDocumentedRoute(t *testing.T) {
	routes := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/api/assets", http.StatusOK},
		{http.MethodGet, "/api/assets/a_1", http.StatusNotFound},
		// Body-less on purpose: the update endpoint is implemented, so the
		// answer comes from decodeJSON refusing an empty body rather than
		// from the 501 stub. Both go through the JSON writer, which is what
		// this test exists to check.
		{http.MethodPatch, "/api/assets/a_1", http.StatusBadRequest},
		// Body-less, against an empty store: the create endpoint is
		// implemented, so the answer is decodeJSON refusing an empty
		// body rather than the 501 stub. The detail endpoint is
		// implemented too, so a well-formed unknown id is a 404 rather
		// than 501. Both still go through the JSON writer, which is what
		// this test exists to check.
		{http.MethodPost, "/api/tasks", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/t_1", http.StatusNotFound},
		// Body-less, against a server with no event bus attached: the
		// stream endpoint is implemented now, so the answer is its own
		// refusal to open a connection that could never deliver an
		// event, rather than the 501 stub. Both still go through the
		// JSON writer, which is what this test exists to check.
		{http.MethodGet, "/api/events", http.StatusServiceUnavailable},
		// Body-less, against an empty store: the audit endpoint is
		// implemented now, so the answer is the empty [] array rather
		// than the 501 stub. It still goes through the JSON writer,
		// which is what this test exists to check.
		{http.MethodGet, "/api/audit", http.StatusOK},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := do(t, rt.method, rt.path)

			if rec.Code != rt.want {
				t.Fatalf("status = %d, want %d (route not registered?)", rec.Code, rt.want)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			// The implemented GET returns a bare array or object; the
			// unimplemented ones return the error envelope. Both must be
			// valid JSON with a non-empty body, which is what proves the
			// answer did not come from ServeMux's plain-text default.
			if !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("body is not valid JSON: %q", rec.Body.String())
			}
		})
	}
}

// TestHandlerUnknownPathIsJSON404 verifies a path outside the documented
// surface answers 404 through the same envelope as everything else, so a
// client never has to parse two error formats.
func TestHandlerUnknownPathIsJSON404(t *testing.T) {
	for _, path := range []string{"/", "/api", "/api/unknown", "/api/unknown/a_1", "/api/assets/a_1/details"} {
		t.Run(path, func(t *testing.T) {
			rec := do(t, http.MethodGet, path)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if got := decodeErr(t, rec).Code; got != codeNotFound {
				t.Fatalf("error code = %q, want %q", got, codeNotFound)
			}
		})
	}
}

// TestHandlerRejectsWrongMethodWithJSON405 verifies a known path with an
// unsupported method answers 405 with the methods it does accept: the Allow
// header is what a client uses to recover without reading documentation.
func TestHandlerRejectsWrongMethodWithJSON405(t *testing.T) {
	cases := []struct {
		method string
		path   string
		allow  string
	}{
		{http.MethodDelete, "/api/assets", http.MethodGet + ", HEAD"},
		{http.MethodPut, "/api/assets/a_1", http.MethodGet + ", HEAD, " + http.MethodPatch},
		{http.MethodDelete, "/api/tasks", http.MethodPost},
		{http.MethodGet, "/api/tasks", http.MethodPost},
		{http.MethodPost, "/api/audit", http.MethodGet + ", HEAD"},
		{http.MethodPatch, "/api/events", http.MethodGet + ", HEAD"},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := do(t, c.method, c.path)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if got := rec.Header().Get("Allow"); got != c.allow {
				t.Fatalf("Allow = %q, want %q", got, c.allow)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if got := decodeErr(t, rec).Code; got != codeMethodNotAllowed {
				t.Fatalf("error code = %q, want %q", got, codeMethodNotAllowed)
			}
		})
	}
}

// TestWrapJSONErrorsPassesSuccessfulResponsesThrough verifies the wrapper does
// not rewrite a good answer: it holds the response back only to decide, and
// forwarding must restore the status, the headers and the body exactly.
func TestWrapJSONErrorsPassesSuccessfulResponsesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Route", "kept")
		writeJSON(w, http.StatusOK, map[string]string{"asset_id": "a_1"})
	})
	rec := httptest.NewRecorder()
	wrapJSONErrors(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Route"); got != "kept" {
		t.Fatalf("X-Route = %q, want the handler's header preserved", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got, want := rec.Body.String(), `{"asset_id":"a_1"}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// TestWrapJSONErrorsAnswersHandlerPanicWithEnvelope verifies a panicking
// handler produces a JSON 500 rather than a dropped connection.
//
// Without recovery net/http closes the connection after logging, leaving the
// client with no status and a truncated body — the one failure mode where a
// client cannot even tell that an error happened.
func TestWrapJSONErrorsAnswersHandlerPanicWithEnvelope(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("handler exploded before writing anything")
	})
	rec := httptest.NewRecorder()
	wrapJSONErrors(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := decodeErr(t, rec).Code; got != codeInternal {
		t.Fatalf("error code = %q, want %q", got, codeInternal)
	}
}

// TestWrapJSONErrorsPassesFlushThrough verifies a streaming handler is not
// turned into a buffered one.
//
// The SSE endpoint of a later task writes an event per flush; if this wrapper
// buffered its body, a WebUI would see the stream arrive in one lump at the
// end instead of growing as state changes, which is the whole point of SSE.
func TestWrapJSONErrorsPassesFlushThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: task_updated\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("first Flush() = %v", err)
		}
		_, _ = w.Write([]byte("data: {}\n\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("second Flush() = %v", err)
		}
	})
	rec := httptest.NewRecorder()
	wrapJSONErrors(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	want := "event: task_updated\ndata: {}\n\n"
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if !rec.Flushed {
		t.Fatal("streaming handler's Flush was not passed through")
	}
}

func TestCaptureWriterPreservesCommitSemanticsAndFlushError(t *testing.T) {
	flushErr := errors.New("flush failed")
	underlying := &flushErrorWriter{err: flushErr}
	capture := &captureWriter{ResponseWriter: underlying}
	capture.Header().Set("X-Before", "kept")
	capture.WriteHeader(http.StatusCreated)
	capture.WriteHeader(http.StatusInternalServerError)
	capture.Header().Set("X-After", "ignored")
	if _, err := capture.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	if err := capture.FlushError(); !errors.Is(err, flushErr) {
		t.Fatalf("FlushError() = %v, want underlying error %v", err, flushErr)
	}
	if underlying.status != http.StatusCreated {
		t.Fatalf("status = %d, want first status %d", underlying.status, http.StatusCreated)
	}
	if got := underlying.Header().Get("X-Before"); got != "kept" {
		t.Fatalf("committed header = %q, want kept", got)
	}
	if got := underlying.Header().Get("X-After"); got != "" {
		t.Fatalf("late header = %q, want it excluded after commit", got)
	}
	if got := string(underlying.body); got != "body" {
		t.Fatalf("body = %q, want body", got)
	}
	if _, ok := any(capture).(http.Flusher); ok {
		t.Fatal("captureWriter advertises http.Flusher without knowing the underlying capability")
	}
	if got := capture.Unwrap(); got != underlying {
		t.Fatal("Unwrap() did not expose the underlying ResponseWriter")
	}
}

func TestWrapJSONErrorsUsesHeadersFrozenAtCommit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Header().Set("Allow", "DELETE")
		_, _ = w.Write([]byte("Method Not Allowed\n"))
	})
	rec := httptest.NewRecorder()
	wrapJSONErrors(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/assets/a_1", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Fatalf("Allow = %q, want frozen value GET", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := decodeErr(t, rec).Code; got != codeMethodNotAllowed {
		t.Fatalf("error code = %q, want %q", got, codeMethodNotAllowed)
	}
}

// TestWrapJSONErrorsKeepsHandlerStatuses verifies a status a handler chose —
// including a 501 and a 409 — is not mistaken for a router answer and
// rewritten. Only 404 and 405 belong to the router.
func TestWrapJSONErrorsKeepsHandlerStatuses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusNotImplemented, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, status, map[string]bool{"kept": true})
			})
			rec := httptest.NewRecorder()
			wrapJSONErrors(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

			if rec.Code != status {
				t.Fatalf("status = %d, want %d preserved", rec.Code, status)
			}
			if got := rec.Body.String(); got != `{"kept":true}` {
				t.Fatalf("body = %q, want the handler's body preserved", got)
			}
		})
	}
}
