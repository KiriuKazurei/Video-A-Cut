package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"net/http"
	"strconv"
)

type profileSaveRequest struct {
	Profile          preparation.Profile `json:"profile"`
	ExpectedRevision int                 `json:"expected_revision"`
	IdempotencyKey   string              `json:"idempotency_key"`
}
type profileRef struct {
	ProfileID            string `json:"profile_id"`
	Revision             int    `json:"revision"`
	ExpectedAssetVersion string `json:"expected_asset_version,omitempty"`
	RevisionID           string `json:"revision_id,omitempty"`
	IdempotencyKey       string `json:"idempotency_key,omitempty"`
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	limit, offset := 20, 0
	var e error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, e = strconv.Atoi(v)
		if e != nil || limit < 1 || limit > 50 {
			writeServiceError(w, model.ErrArgument)
			return
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, e = strconv.Atoi(v)
		if e != nil || offset < 0 || offset > 100000 {
			writeServiceError(w, model.ErrArgument)
			return
		}
	}
	p, e := s.svc.ListProcessingProfiles(r.Context(), limit, offset)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) saveProfile(w http.ResponseWriter, r *http.Request) {
	var req profileSaveRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	if id := r.PathValue("id"); id != "" && id != req.Profile.ProfileID {
		writeServiceError(w, model.ErrArgument)
		return
	}
	p, e := s.svc.SaveProcessingProfile(r.Context(), humanActor, req.IdempotencyKey, req.ExpectedRevision, req.Profile)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 201, p)
}
func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	revision, e := strconv.Atoi(r.PathValue("revision"))
	if e != nil || revision < 0 {
		writeServiceError(w, model.ErrArgument)
		return
	}
	p, sum, e := s.svc.GetProcessingProfile(r.Context(), r.PathValue("id"), revision)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"profile": p, "profile_sha256": sum})
}
func (s *Server) profileConsent(w http.ResponseWriter, r *http.Request) {
	revision, e := strconv.Atoi(r.PathValue("revision"))
	if e != nil || revision < 1 {
		writeServiceError(w, model.ErrArgument)
		return
	}
	e = s.svc.SetProcessingConsent(r.Context(), humanActor, r.PathValue("id"), revision, r.Method == "POST")
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"granted": r.Method == "POST"})
}
func (s *Server) preparedPreflight(w http.ResponseWriter, r *http.Request) {
	var req profileRef
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	if req.Revision < 1 {
		writeServiceError(w, model.ErrArgument)
		return
	}
	report, e := s.svc.PreparedPreflight(r.Context(), r.PathValue("id"), req.ProfileID, req.Revision)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, report)
}
func (s *Server) startPrepared(w http.ResponseWriter, r *http.Request) {
	var req profileRef
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	run, e := s.svc.StartPreparedWorkflow(r.Context(), humanActor, preparation.PreparedStart{AssetID: r.PathValue("id"), ProfileID: req.ProfileID, ProfileRevision: req.Revision, ExpectedAssetVersion: req.ExpectedAssetVersion, RevisionID: req.RevisionID, IdempotencyKey: req.IdempotencyKey})
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 201, run)
}
