package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"net/http"
	"strconv"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

type startWorkflowRequest struct {
	RevisionID     string `json:"revision_id"`
	IdempotencyKey string `json:"idempotency_key"`
	ContentMode    string `json:"content_mode"`
}

type versionRequest struct {
	ExpectedVersion int      `json:"expected_version"`
	Reason          string   `json:"reason"`
	FailedStage     string   `json:"failed_stage"`
	IdempotencyKey  string   `json:"idempotency_key"`
	RevisionID      string   `json:"revision_id"`
	BaseRevisionID  string   `json:"base_revision_id"`
	SceneID         string   `json:"scene_id"`
	Decision        string   `json:"decision"`
	Note            string   `json:"note"`
	NarrationID     string   `json:"narration_id"`
	Label           string   `json:"label"`
	SequenceRank    *int     `json:"sequence_rank"`
	Text            string   `json:"text"`
	Start           *float64 `json:"start"`
	End             *float64 `json:"end"`
	SourceSceneID   string   `json:"source_scene_id"`
	CheckItem       string   `json:"check_item"`
	Result          string   `json:"result"`
}

func (s *Server) startWorkflow(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var req startWorkflowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	run, err := s.svc.StartWorkflow(r.Context(), humanActor, id, req.RevisionID, req.IdempotencyKey, req.ContentMode)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	limit, offset := 20, 0
	if value := r.URL.Query().Get("limit"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil {
			writeServiceError(w, model.ErrArgument)
			return
		}
		limit = n
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil {
			writeServiceError(w, model.ErrArgument)
			return
		}
		offset = n
	}
	runs, err := s.svc.ListWorkflowsPage(r.Context(), id, limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	run, stages, err := s.svc.GetWorkflow(r.Context(), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	tasks := []model.Task{}
	for _, stage := range stages {
		task, err := s.svc.GetTask(r.Context(), stage.TaskID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		tasks = append(tasks, task)
	}
	binding, e := s.svc.WorkflowProfileInfo(r.Context(), run.RunID)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run, "stages": stages, "tasks": tasks, "profile_binding": binding})
}

func (s *Server) cancelWorkflow(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	run, err := s.svc.CancelWorkflow(r.Context(), humanActor, r.PathValue("run"), req.Reason, req.ExpectedVersion)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) retryWorkflow(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	run, err := s.svc.RetryWorkflow(r.Context(), humanActor, r.PathValue("run"), req.FailedStage, req.IdempotencyKey, req.ExpectedVersion)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) reviewWorkflow(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Review(r.Context(), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) editWorkflow(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	base := req.BaseRevisionID
	if base == "" {
		base = req.RevisionID
	}
	run, err := s.svc.EditWorkflow(r.Context(), humanActor, r.PathValue("run"), req.ExpectedVersion, base,
		req.SceneID, req.Label, req.SequenceRank, req.NarrationID, req.Text, req.Start, req.End, req.SourceSceneID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) reviewScene(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	run, err := s.svc.ConfirmScene(r.Context(), humanActor, r.PathValue("run"), req.ExpectedVersion, req.RevisionID, req.SceneID, req.Decision, req.Note)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) reviewNarration(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	run, err := s.svc.ApproveWorkflowNarration(r.Context(), humanActor, r.PathValue("run"), req.ExpectedVersion, req.RevisionID, req.NarrationID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) revokeWorkflowNarration(w http.ResponseWriter, r *http.Request) {
	version, _ := strconv.Atoi(r.URL.Query().Get("expected_version"))
	run, err := s.svc.RevokeWorkflowNarration(r.Context(), humanActor, r.PathValue("run"), r.PathValue("narration"), version)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) getAcceptance(w http.ResponseWriter, r *http.Request) {
	rows, err := s.svc.ListAcceptance(r.Context(), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) postAcceptance(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	rec, err := s.svc.RecordAcceptance(r.Context(), humanActor, r.PathValue("run"), req.CheckItem, req.Result, req.Note, req.ExpectedVersion)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// Keep the service import used when the compiler drops unused names during edits.
var _ = service.TaskInput{}
