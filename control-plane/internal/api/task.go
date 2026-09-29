package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// createTaskRequest is the body of POST /api/tasks: the four fields a caller
// names and nothing else.
//
// The type is closed on purpose. model.Task is the whole stored row — status,
// progress, agent_id, the lease, the claimed-at stamp, the artifacts — and
// decoding into it would make every one of those settable over REST by anyone
// who can reach this surface. A task is the unit agents are paid in, so the
// write path and the read path are different sets by construction: the read
// path is GET /api/tasks/{id}, and the write path is these four fields plus
// the two numbers the server owns (status, updated_at).
//
// decodeJSON sets DisallowUnknownFields, so a body carrying status,
// progress, agent_id, lease_expires_at, claimed_at, updated_at or artifacts is
// refused as one 400 rather than silently ignored. Silence would be the worse
// behavior: a client that filled those fields out of a GET's response body —
// the natural way to copy a stored row and resubmit it — would believe it had
// created a finished or already-claimed task.
//
// The fields are plain strings, not pointers: all four are required, so there
// is no "absent but settable later" case to distinguish. A missing JSON key
// and an explicit "" both have to be refused, which is what the guard in
// createTask does.
type createTaskRequest struct {
	TaskID    string `json:"task_id"`
	AssetID   string `json:"asset_id"`
	Type      string `json:"type"`
	AgentRole string `json:"agent_role"`
	// DependsOn is optional: task ids on the same asset that must succeed
	// before this one can be claimed.
	DependsOn []string `json:"depends_on,omitempty"`
}

// task builds the model.Task the request describes.
//
// Only the four settable fields are carried across; every other member keeps
// its zero value, which is what makes the ownership of those fields explicit
// at the call site. Status is left to the service, which forces it to queued;
// AgentID, Progress, Message and the lease are left to the agent-side calls
// that own the task's execution state.
func (r createTaskRequest) task() model.Task {
	return model.Task{
		TaskID:    r.TaskID,
		AssetID:   r.AssetID,
		Type:      r.Type,
		AgentRole: r.AgentRole,
		DependsOn: r.DependsOn,
	}
}

// createTask answers POST /api/tasks by queueing one new unit of work on an
// existing asset.
//
// It is the human dispatch surface. It holds no rule of its own: the request
// type decides what is nameable, service.CreateTask decides whether the task is
// legal (the asset must exist, the ids and role must be non-empty, the status is
// forced to queued), stores it, publishes task_created and audits it.
//
// The response is 201 with the stored row rather than an echo of the request:
// the queue the WebUI renders after a dispatch reads status, updated_at and the
// server timestamps from this same answer, and echoing the request would show
// a row that does not exist yet. The row is read back through
// service.CreateTask + GetTask rather than returned from the write call because
// CreateTask returns only an error — so the canonical row is the store's, not
// this handler's reconstruction of it.
//
// service.CreateTask takes no actor: it audits under the fixed "system" actor
// because a queue entry is created by the pipeline rather than by a person.
// The humanActor constant is therefore deliberately unused here — spreading a
// human actor over a task creation would file a machine event as a governance
// event, which is the opposite of what the retention sweep's classification
// exists to keep straight.
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		// decodeJSON has already classified the failure: a malformed or
		// empty body is model.ErrArgument, an oversize one
		// errBodyTooLarge, so the mapping to 400 and 413 is done in one
		// place.
		writeServiceError(w, err)
		return
	}

	// Report missing request fields together at the transport boundary for a
	// useful client error. service.CreateTask repeats the business validation
	// for non-HTTP callers and owns the queued task's state rules.
	var missing []string
	if req.TaskID == "" {
		missing = append(missing, "task_id")
	}
	if req.AssetID == "" {
		missing = append(missing, "asset_id")
	}
	if req.Type == "" {
		missing = append(missing, "type")
	}
	if req.AgentRole == "" {
		missing = append(missing, "agent_role")
	}
	if len(missing) > 0 {
		writeError(w, http.StatusBadRequest, codeArgument,
			fmt.Sprintf("task requires %s", strings.Join(missing, ", ")))
		return
	}

	if err := s.svc.CreateTask(r.Context(), req.task()); err != nil {
		writeServiceError(w, err)
		return
	}

	// Read the row back: the write call returns no task, and the stored
	// row is the only authoritative copy of what was queued (the forced
	// queued status, the store's updated_at stamp, the cleared execution
	// fields). A 500 here would mean the create succeeded but the answer
	// could not be built, so the client is told so rather than left to
	// assume.
	tk, err := s.svc.GetTask(r.Context(), req.TaskID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tk)
}

// getTask answers GET /api/tasks/{id} with one task.
//
// It is the human read surface and applies no visibility rule of its own: the
// only decision point for "what may an agent see" is service.ListVisibleAssets
// (§11.2), and filtering here would be the second copy of that rule the docs
// warn about. A WebUI task view has to show a claimed, running or failed task
// as readily as a queued one, or an operator could not see the queue they are
// responsible for.
//
// The id guard is identical to getAsset's, including why it exists:
// r.PathValue returns the URL-decoded segment, so an escaped separator
// (t%2F1) reaches this handler as "t/1". The store would look such a value up
// as a literal and answer not found, but 400 says the truth — this is a
// malformed id, not a missing one — and a caller that sees a 404 has no way to
// learn the difference. Keeping the separator out of the query also means no
// value derived from the URL can be assembled into a path.
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, codeArgument, "task id is required")
		return
	}
	// Both separators are checked, not just "/": "\\" separates path
	// components on Windows, and an id carrying either is equally
	// malformed.
	if strings.ContainsAny(id, "/\\") {
		writeError(w, http.StatusBadRequest, codeArgument,
			"task id must not contain a path separator")
		return
	}

	tk, err := s.svc.GetTask(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tk)
}
