package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"net/http"
)

func (s *Server) diagnoseProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider preparation.Provider `json:"provider"`
		APIKey   string               `json:"api_key,omitempty"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	result, err := s.svc.DiagnoseProvider(r.Context(), req.Provider, req.APIKey, r.PathValue("operation"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
