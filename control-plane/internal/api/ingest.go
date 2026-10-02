package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

type registerRecordingRequest struct {
	AssetID        string `json:"asset_id"`
	RootID         string `json:"root_id"`
	RelativePath   string `json:"relative_path"`
	IdempotencyKey string `json:"idempotency_key"`
}

type startIngestRequest struct {
	SourceID              string `json:"source_id"`
	ExpectedSourceVersion string `json:"expected_source_version"`
	IdempotencyKey        string `json:"idempotency_key"`
	ResourcePolicy        string `json:"resource_policy,omitempty"`
}

type ingestCancelRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	Reason          string `json:"reason"`
}

type ingestRetryRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	Stage           string `json:"stage"`
	IdempotencyKey  string `json:"idempotency_key"`
	ResourcePolicy  string `json:"resource_policy,omitempty"`
}

func pageArgs(r *http.Request, defLimit int) (int, int, error) {
	limit, offset := defLimit, 0
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil {
			return 0, 0, fmt.Errorf("limit must be an integer: %w", model.ErrArgument)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if offset, err = strconv.Atoi(v); err != nil {
			return 0, 0, fmt.Errorf("offset must be an integer: %w", model.ErrArgument)
		}
	}
	return limit, offset, nil
}

func (s *Server) listIngestRoots(w http.ResponseWriter, r *http.Request) {
	roots, err := s.svc.ListIngestRoots(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roots)
}

func (s *Server) registerRecording(w http.ResponseWriter, r *http.Request) {
	var req registerRecordingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.svc.RegisterRecording(r.Context(), humanActor, req.AssetID, req.RootID, req.RelativePath, req.IdempotencyKey)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listRecordings(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	out, err := s.svc.ListRecordingSources(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) startIngest(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var req startIngestRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.svc.StartIngestRunWithPolicy(r.Context(), humanActor, id, req.SourceID, req.ExpectedSourceVersion, req.IdempotencyKey, req.ResourcePolicy)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) listIngestRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	limit, offset, err := pageArgs(r, 20)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	runs, err := s.svc.ListIngestRuns(r.Context(), id, limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getIngestRun(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.GetIngestRunView(r.Context(), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) listIngestSegments(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pageArgs(r, 50)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	page, err := s.svc.ListIngestSegments(r.Context(), r.PathValue("run"), limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) postAnalysisPlan(w http.ResponseWriter, r *http.Request) {
	var req service.AnalysisRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.svc.SubmitAnalysisPlan(r.Context(), humanActor, r.PathValue("run"), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) postSelection(w http.ResponseWriter, r *http.Request) {
	var req service.SelectionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.svc.SubmitSelection(r.Context(), humanActor, r.PathValue("run"), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) postPrepare(w http.ResponseWriter, r *http.Request) {
	var req service.PrepareRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.svc.PrepareIngest(r.Context(), humanActor, r.PathValue("run"), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) cancelIngest(w http.ResponseWriter, r *http.Request) {
	var req ingestCancelRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.svc.CancelIngestRun(r.Context(), humanActor, r.PathValue("run"), req.ExpectedVersion, req.Reason)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) retryIngest(w http.ResponseWriter, r *http.Request) {
	var req ingestRetryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.svc.RetryIngestRunWithPolicy(r.Context(), humanActor, r.PathValue("run"), req.ExpectedVersion, req.Stage, req.IdempotencyKey, req.ResourcePolicy)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

// getIngestFile serves a registered thumbnail, prepared clip or the probe
// summary. The key is a registered name, never a path.
func (s *Server) getIngestFile(w http.ResponseWriter, r *http.Request) {
	run, key := r.PathValue("run"), r.PathValue("key")
	if !validDeliveryID(run) || !validDeliveryID(key) {
		writeServiceError(w, fmt.Errorf("invalid ingest file address: %w", model.ErrArgument))
		return
	}
	if key == "probe" {
		pr, err := s.svc.IngestProbeJSON(r.Context(), run)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}
	f, file, err := s.svc.OpenIngestFile(r.Context(), run, key)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Type", file.Mime)
	if !file.Playable {
		h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", key+filepath.Ext(file.Name)))
	}
	info, err := f.Stat()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	http.ServeContent(w, r, file.Name, info.ModTime(), f)
}
