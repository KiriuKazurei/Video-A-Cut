package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"net/http"
)

func (s *Server) preflightWorkflow(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var profile preparation.Profile
	if err := decodeJSON(w, r, &profile); err != nil {
		writeServiceError(w, err)
		return
	}
	report, err := s.svc.PreflightWorkflow(r.Context(), id, profile)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
