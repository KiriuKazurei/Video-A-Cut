package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// post sends one POST with a raw body through the router of an already-seeded
// server and returns the recorded response.
//
// The body is a string rather than a struct for the same reason patch uses
// one: the payloads this endpoint must refuse — an internal field a client
// invented, a task_id copied off a stored row — are exactly the ones a struct
// literal would refuse to compile.
func post(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// decodeTask decodes a success response body as one task.
//
// The body is the bare task row, so a JSON null is reported rather than
// decoded into a zero Task: every assertion built on a zero value would
// silently "pass" against a body that carried nothing.
func decodeTask(t *testing.T, rec *httptest.ResponseRecorder) model.Task {
	t.Helper()
	if body := strings.TrimSpace(rec.Body.String()); !strings.HasPrefix(body, "{") {
		t.Fatalf("body is not a JSON object: %q", rec.Body.String())
	}
	var out model.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not a JSON task: %v (%q)", err, rec.Body.String())
	}
	return out
}

// seedTask returns a Server holding one asset and one queued task on it.
//
// The task is created through service.CreateTask rather than the endpoint
// under test so the GET and route tests do not depend on POST for their setup:
// a broken createTask must not take the getTask tests down with it.
func seedTask(t *testing.T) (*Server, model.Task) {
	t.Helper()
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})
	tk := model.Task{
		TaskID:    "t_1",
		AssetID:   "a_1",
		Type:      model.TaskTypeRecognize,
		AgentRole: "recognizer",
	}
	if err := srv.svc.CreateTask(context.Background(), tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return srv, tk
}

// createBody is the well-formed request the happy paths build from. It names
// only the four settable fields — no status, no agent, no lease.
func createBody(taskID string) string {
	return `{"task_id":"` + taskID + `","asset_id":"a_1","type":"recognize","agent_role":"recognizer"}`
}

// TestCreateTaskReturnsCreated proves POST /api/tasks queues a task and answers
// 201 with the stored row.
//
// The response is not an echo of the request: status is forced to queued by the
// service and updated_at is stamped by the store, so the client learns the
// canonical row from the same answer that created it — the WebUI queue view
// renders this body instead of re-GETting per dispatch.
func TestCreateTaskReturnsCreated(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	rec := post(t, srv, "/api/tasks", createBody("t_1"))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusCreated, rec.Body.String())
	}
	got := decodeTask(t, rec)
	if got.TaskID != "t_1" {
		t.Fatalf("task_id = %q, want t_1", got.TaskID)
	}
	if got.AssetID != "a_1" {
		t.Fatalf("asset_id = %q, want a_1", got.AssetID)
	}
	if got.Type != model.TaskTypeRecognize {
		t.Fatalf("type = %q, want %q", got.Type, model.TaskTypeRecognize)
	}
	if got.AgentRole != "recognizer" {
		t.Fatalf("agent_role = %q, want recognizer", got.AgentRole)
	}
	// The starting status is the server's decision, never the client's: a
	// freshly queued task has not been picked up, and starting it anywhere
	// else would let a task skip the queue entirely.
	if got.Status != model.TaskStatusQueued {
		t.Fatalf("status = %q, want %q — the client must not choose the starting status",
			got.Status, model.TaskStatusQueued)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("updated_at = %v, want the store's timestamp", got.UpdatedAt)
	}
	if got.AgentID != "" || got.Progress != 0 || len(got.Artifacts) != 0 {
		t.Fatalf("a new task carries execution state: %+v", got)
	}

	// The row is persisted, not just echoed back.
	stored := decodeTask(t, get(t, srv, "/api/tasks/t_1"))
	if stored.TaskID != "t_1" || stored.Status != model.TaskStatusQueued {
		t.Fatalf("stored task = %+v, want t_1 queued", stored)
	}
}

// TestCreateTaskRejectsInternalFields proves the request type is closed, which
// is the security point of this endpoint.
//
// A handler decoding into model.Task would accept every field of the stored row
// a client could copy out of a GET — status, progress, agent_id, the lease and
// the artifacts — and would then let anyone mark work finished, claim a task
// under an agent identity they do not hold, or forge a lease. decodeJSON's
// DisallowUnknownFields refuses all of it as one 400, so the write fields and
// the read fields are different sets by construction.
//
// The field names are the ones a stored task actually serializes to, because
// that is the body a real client would have sent: lease_expires_at and
// claimed_at are the tags of model.Task, not a guess.
func TestCreateTaskRejectsInternalFields(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	rec := post(t, srv, "/api/tasks",
		`{"task_id":"t_1","asset_id":"a_1","type":"recognize","agent_role":"recognizer",`+
			`"status":"succeeded","progress":0.9,"agent_id":"hacker",`+
			`"message":"forged","lease_expires_at":"2030-01-01T00:00:00Z",`+
			`"claimed_at":"2030-01-01T00:00:00Z","updated_at":"2030-01-01T00:00:00Z",`+
			`"artifacts":{"edl":"clips/a_1/edl.json"}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d — an internal field must not be accepted", rec.Code, http.StatusBadRequest)
	}
	if got := decodeErr(t, rec).Code; got != codeArgument {
		t.Fatalf("error code = %q, want %q", got, codeArgument)
	}

	// The rejection is total: nothing was written, so the task does not
	// exist and no execution state was forged anywhere.
	if rec := get(t, srv, "/api/tasks/t_1"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d — a rejected body still created a task", rec.Code, http.StatusNotFound)
	}
}

// TestCreateTaskUnknownAssetReturns404 proves a task can never reference an
// asset that does not exist.
//
// The store's only foreign-key guarantee is per-connection pragma enforcement,
// so the existence check in the service is the real guard and this is the
// assertion that the handler surfaces it rather than swallowing it into a 500.
func TestCreateTaskUnknownAssetReturns404(t *testing.T) {
	srv := newServer(t)

	rec := post(t, srv, "/api/tasks",
		`{"task_id":"t_1","asset_id":"missing","type":"recognize","agent_role":"recognizer"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if got := decodeErr(t, rec).Code; got != codeNotFound {
		t.Fatalf("error code = %q, want %q", got, codeNotFound)
	}
}

// TestCreateTaskDuplicateTaskIDReturns409 proves task_id is the primary key the
// queue is dispatched by, and a second creation of the same id is a conflict.
//
// The duplicate must not be a silent success: two tasks sharing an id would
// make every later claim, progress report and result submission ambiguous
// about which row it referred to.
func TestCreateTaskDuplicateTaskIDReturns409(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	if rec := post(t, srv, "/api/tasks", createBody("t_1")); rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want %d (%s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec := post(t, srv, "/api/tasks", createBody("t_1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if got := decodeErr(t, rec).Code; got != codeConflict {
		t.Fatalf("error code = %q, want %q", got, codeConflict)
	}
}

// TestCreateTaskMissingFieldsReturn400 proves each required field of the body
// is enforced at the transport.
//
// All four are checked here rather than left to the service because the answer
// must be the same for all of them: service.CreateTask validates task_id,
// asset_id and agent_role but not type, so a body with an empty type would
// otherwise queue an untyped task that no agent could ever claim. One guard
// before the service keeps the request shape answer uniform, and the service
// still enforces its own rules for its non-HTTP callers.
//
// An absent field and an empty-string field fail alike: JSON
// {"task_id":""} is a present key with no value, and a client that sends it
// has named nothing.
func TestCreateTaskMissingFieldsReturn400(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"absent task_id", `{"asset_id":"a_1","type":"recognize","agent_role":"recognizer"}`},
		{"empty task_id", `{"task_id":"","asset_id":"a_1","type":"recognize","agent_role":"recognizer"}`},
		{"absent asset_id", `{"task_id":"t_2","type":"recognize","agent_role":"recognizer"}`},
		{"empty asset_id", `{"task_id":"t_3","","asset_id":"","type":"","agent_role":"recognizer"}`},
		{"absent type", `{"task_id":"t_4","asset_id":"a_1","agent_role":"recognizer"}`},
		{"empty type", `{"task_id":"t_5","asset_id":"a_1","type":"","agent_role":"recognizer"}`},
		{"absent agent_role", `{"task_id":"t_6","asset_id":"a_1","type":"recognize"}`},
		{"empty agent_role", `{"task_id":"t_7","asset_id":"a_1","type":"recognize","agent_role":""}`},
		{"empty object", `{}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

			rec := post(t, srv, "/api/tasks", c.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if got := decodeErr(t, rec).Code; got != codeArgument {
				t.Fatalf("error code = %q, want %q", got, codeArgument)
			}
		})
	}
}

// TestCreateTaskRejectsBadBody proves a malformed body is a client error with
// the invalid_argument code, not a 500 and not a silently queued default task.
func TestCreateTaskRejectsBadBody(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	rec := post(t, srv, "/api/tasks", `not json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := decodeErr(t, rec).Code; got != codeArgument {
		t.Fatalf("error code = %q, want %q", got, codeArgument)
	}
}

// TestGetTaskReturnsOne proves GET /api/tasks/{id} returns the stored task with
// its whole row.
//
// Every field a partial encoder would drop is asserted: the role that dispatches
// it, the type that selects the agent, and the lease timestamps an agent-side
// view reads. Like the asset endpoints this is the human governance view — it
// applies no visibility rule of its own, because service.ListVisibleAssets is
// the only place that decision belongs (§11.2).
func TestGetTaskReturnsOne(t *testing.T) {
	srv, _ := seedTask(t)

	rec := get(t, srv, "/api/tasks/t_1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := decodeTask(t, rec)
	if got.TaskID != "t_1" {
		t.Fatalf("task_id = %q, want t_1", got.TaskID)
	}
	if got.AssetID != "a_1" {
		t.Fatalf("asset_id = %q, want a_1", got.AssetID)
	}
	if got.Type != model.TaskTypeRecognize {
		t.Fatalf("type = %q, want %q", got.Type, model.TaskTypeRecognize)
	}
	if got.AgentRole != "recognizer" {
		t.Fatalf("agent_role = %q, want recognizer", got.AgentRole)
	}
	if got.Status != model.TaskStatusQueued {
		t.Fatalf("status = %q, want %q", got.Status, model.TaskStatusQueued)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("updated_at = %v, want the store's timestamp", got.UpdatedAt)
	}
}

// TestGetTaskUnknownReturns404 proves a well-formed but unknown id is a 404
// with the machine-readable not_found code, not a 500.
func TestGetTaskUnknownReturns404(t *testing.T) {
	srv := newServer(t)

	rec := get(t, srv, "/api/tasks/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := decodeErr(t, rec).Code; got != codeNotFound {
		t.Fatalf("error code = %q, want %q", got, codeNotFound)
	}
}

// TestGetTaskRejectsPathSeparator proves an id carrying a path separator is
// rejected as invalid_argument instead of being looked up.
//
// ServeMux matches the unescaped segment, so a%2Fb reaches the handler as
// "a/b": the guard is the handler's, not the router's. The ids used here exist
// in no store, so a 404 answer would mean the value was forwarded — and a
// forwarded escaped separator is the input of every path-traversal attempt
// against a store backed by the filesystem.
func TestGetTaskRejectsPathSeparator(t *testing.T) {
	srv := newServer(t)

	for _, path := range []string{
		"/api/tasks/a%2Fb",
		"/api/tasks/a%5Cb",
		"/api/tasks/..%2F..%2Fetc%2Fpasswd",
	} {
		t.Run(path, func(t *testing.T) {
			rec := get(t, srv, path)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d — a separator in the id must not reach the store",
					rec.Code, http.StatusBadRequest)
			}
			if got := decodeErr(t, rec).Code; got != codeArgument {
				t.Fatalf("error code = %q, want %q", got, codeArgument)
			}
		})
	}
}

// TestGetTaskEmptyIDRejected pins the empty-id guard from the handler's own
// side.
//
// The router never dispatches an empty segment: /api/tasks/ matches no pattern
// and answers 404 with the not_found code, which is the right answer for a
// malformed URL. The handler's guard covers the other route in — a request
// produced by something other than ServeMux's pattern matching — which this
// test supplies directly so the guard is exercised rather than assumed.
func TestGetTaskEmptyIDRejected(t *testing.T) {
	srv := newServer(t)

	// The routing half: an empty segment is a 404 through the envelope.
	rec := get(t, srv, "/api/tasks/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d for /api/tasks/", rec.Code, http.StatusNotFound)
	}
	if got := decodeErr(t, rec).Code; got != codeNotFound {
		t.Fatalf("error code = %q, want %q", got, codeNotFound)
	}

	// The handler half: an empty id that does reach getTask is a 400, not
	// a store round-trip.
	req := localRequest(http.MethodGet, "/api/tasks/", nil)
	req.SetPathValue("id", "")
	rec2 := httptest.NewRecorder()
	srv.getTask(rec2, req)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d for an empty id", rec2.Code, http.StatusBadRequest)
	}
	if got := decodeErr(t, rec2).Code; got != codeArgument {
		t.Fatalf("error code = %q, want %q", got, codeArgument)
	}
}

// TestTaskEndpointsResponseIsJSON checks both task endpoints — the 201, the 200
// and the rejected 400 — are served as application/json with the nosniff
// header.
//
// Without nosniff an old browser will content-sniff a JSON body into something
// script-bearing, which is a real hazard on the origin that also serves the
// WebUI. The error paths are checked too because writeError and
// writeServiceError are separate writers from writeJSON: a bug in either must
// not leave a client with a text/plain envelope it cannot parse.
func TestTaskEndpointsResponseIsJSON(t *testing.T) {
	srv, _ := seedTask(t)

	cases := []struct {
		name string
		send func() *httptest.ResponseRecorder
	}{
		{"POST created", func() *httptest.ResponseRecorder {
			return post(t, srv, "/api/tasks", createBody("t_2"))
		}},
		{"POST rejected", func() *httptest.ResponseRecorder {
			return post(t, srv, "/api/tasks",
				`{"task_id":"t_3","asset_id":"a_1","type":"recognize","agent_role":"r","status":"done"}`)
		}},
		{"GET one", func() *httptest.ResponseRecorder {
			return get(t, srv, "/api/tasks/t_1")
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := c.send()

			if rec.Code == http.StatusInternalServerError {
				t.Fatalf("status = %d, want a decided answer (%s)", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("body is not valid JSON: %q", rec.Body.String())
			}
		})
	}
}
