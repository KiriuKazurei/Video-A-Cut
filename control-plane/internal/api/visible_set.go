package api

import (
	"net/http"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

type confirmVisibleSetRequest struct {
	VisibleSetSHA string `json:"visible_set_sha"`
}

type batchVisibilityRequest struct {
	AssetIDs     []string `json:"asset_ids"`
	AgentVisible *bool    `json:"agent_visible"`
}

func (s *Server) getVisibleSet(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.VisibleSet(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) confirmVisibleSet(w http.ResponseWriter, r *http.Request) {
	var req confirmVisibleSetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	view, err := s.svc.ConfirmVisibleSet(r.Context(), humanActor, req.VisibleSetSHA, false)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) batchVisibility(w http.ResponseWriter, r *http.Request) {
	var req batchVisibilityRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	if req.AgentVisible == nil {
		writeServiceError(w, model.ErrArgument)
		return
	}
	assets, err := s.svc.BatchSetAgentVisible(r.Context(), humanActor, req.AssetIDs, *req.AgentVisible)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *Server) enterScript(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, "/\\") {
		writeServiceError(w, model.ErrArgument)
		return
	}
	sc, err := s.svc.EnterScript(r.Context(), humanActor, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (s *Server) listScripts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, "/\\") {
		writeServiceError(w, model.ErrArgument)
		return
	}
	list, err := s.svc.ListScripts(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) finalizeScript(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, "/\\") {
		writeServiceError(w, model.ErrArgument)
		return
	}
	sc, err := s.svc.FinalizeScript(r.Context(), humanActor, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}
