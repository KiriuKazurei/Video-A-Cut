package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestWriteJSONWritesStatusTypeAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]bool{"ok": true})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got, want := rec.Body.String(), `{"ok":true}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// TestWriteJSONFallsBackWhenValueCannotEncode verifies a payload the API
// itself got wrong still yields a consistent JSON 500 rather than a 200 with
// half a body written.
func TestWriteJSONFallsBackWhenValueCannotEncode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, make(chan int)) // json cannot encode a channel

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := decodeErr(t, rec).Code; got != codeInternal {
		t.Fatalf("error code = %q, want %q", got, codeInternal)
	}
}

// TestStatusForMapsSentinelToStatus pins the whole service-to-HTTP mapping in
// one table. Wrapped errors must map the same as bare ones, because every
// layer adds its own %w hop on the way out.
func TestStatusForMapsSentinelToStatus(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"not found", model.ErrNotFound, http.StatusNotFound},
		{"wrapped not found", fmt.Errorf("api: asset %s: %w", "a_1", model.ErrNotFound), http.StatusNotFound},
		{"argument", model.ErrArgument, http.StatusBadRequest},
		{"forbidden", model.ErrForbidden, http.StatusForbidden},
		{"conflict", model.ErrConflict, http.StatusConflict},
		{"invalid state", model.ErrInvalidState, http.StatusConflict},
		{"lease expired", model.ErrLeaseExpired, http.StatusConflict},
		{"lease held", model.ErrLeaseHeld, http.StatusConflict},
		{"body too large", errBodyTooLarge, http.StatusRequestEntityTooLarge},
		{"unclassified", errors.New("sqlite: database is locked"), http.StatusInternalServerError},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := statusFor(c.err); got != c.status {
				t.Fatalf("statusFor(%v) = %d, want %d", c.err, got, c.status)
			}
		})
	}
}

// TestCodeForNamesEveryFailure verifies each sentinel gets its own stable
// code, which is the value a client is allowed to branch on.
func TestCodeForNamesEveryFailure(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{model.ErrNotFound, codeNotFound},
		{model.ErrArgument, codeArgument},
		{model.ErrForbidden, codeForbidden},
		{model.ErrConflict, codeConflict},
		{model.ErrInvalidState, codeInvalidState},
		{model.ErrLeaseExpired, codeLeaseExpired},
		{model.ErrLeaseHeld, codeLeaseHeld},
		{errBodyTooLarge, codeBodyTooLarge},
		{errors.New("boom"), codeInternal},
	}

	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			if got := codeFor(c.err); got != c.code {
				t.Fatalf("codeFor(%v) = %q, want %q", c.err, got, c.code)
			}
		})
	}
}

// TestWriteServiceErrorWritesEnvelope verifies one service error reaches the
// client as status, code and explanation in a single shape.
func TestWriteServiceErrorWritesEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	writeServiceError(rec, fmt.Errorf("service: approve asset %s: %w", "a_1", model.ErrNotFound))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	body := decodeErr(t, rec)
	if body.Code != codeNotFound {
		t.Fatalf("error code = %q, want %q", body.Code, codeNotFound)
	}
	if !strings.Contains(body.Message, "a_1") {
		t.Fatalf("message = %q, want it to name the failing asset", body.Message)
	}
}

// TestWriteServiceErrorHidesInternalFailureDetail verifies an unclassified
// failure answers with the generic body: its message can name paths, SQL and
// hostnames, none of which belong in a response.
func TestWriteServiceErrorHidesInternalFailureDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	writeServiceError(rec, errors.New("sqlite: cannot open /srv/vac.db"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	body := decodeErr(t, rec)
	if body.Code != codeInternal {
		t.Fatalf("error code = %q, want %q", body.Code, codeInternal)
	}
	if body.Message != msgInternal {
		t.Fatalf("message = %q, want %q", body.Message, msgInternal)
	}
}

// TestDecodeJSONReadsWellFormedBody is the happy path every handler builds on.
func TestDecodeJSONReadsWellFormedBody(t *testing.T) {
	body := `{"task_id":"t_1","asset_id":"a_1","type":"recognize","agent_role":"recognizer"}`
	r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))

	var got model.Task
	if err := decodeJSON(httptest.NewRecorder(), r, &got); err != nil {
		t.Fatalf("decodeJSON: %v", err)
	}
	if got.TaskID != "t_1" || got.AssetID != "a_1" {
		t.Fatalf("decoded task = %+v, want task_id/asset_id filled in", got)
	}
	if got.AgentRole != "recognizer" {
		t.Fatalf("decoded agent_role = %q, want %q", got.AgentRole, "recognizer")
	}
}

// TestDecodeJSONRejectsBadBodies verifies every malformed request is a client
// error the handler can answer with 400, and never a partially filled value
// it might act on.
func TestDecodeJSONRejectsBadBodies(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"empty", "", model.ErrArgument},
		{"truncated json", `{"task_id":`, model.ErrArgument},
		{"wrong field type", `{"task_id":7}`, model.ErrArgument},
		{"unknown field", `{"task_id":"t_1","nope":true}`, model.ErrArgument},
		{"two values", `{"task_id":"t_1"}{"task_id":"t_2"}`, model.ErrArgument},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(c.body))
			var got model.Task
			err := decodeJSON(httptest.NewRecorder(), r, &got)
			if !errors.Is(err, c.want) {
				t.Fatalf("decodeJSON: got %v, want %v", err, c.want)
			}
		})
	}
}

// TestDecodeJSONRejectsOversizedBody verifies a body past the cap surfaces as
// errBodyTooLarge, which maps to 413, rather than being silently truncated
// into a shorter valid-looking request.
func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	body := `{"task_id":"` + strings.Repeat("a", maxBodyBytes+16) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))

	var got model.Task
	err := decodeJSON(httptest.NewRecorder(), r, &got)
	if !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("decodeJSON: got %v, want errBodyTooLarge", err)
	}
	if statusFor(err) != http.StatusRequestEntityTooLarge {
		t.Fatalf("statusFor = %d, want %d", statusFor(err), http.StatusRequestEntityTooLarge)
	}
}
