package api

import (
	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"net/http"
)

func (s *Server) getSubtitleStyle(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.GetSubtitleStyle(r.Context(), r.PathValue("run"))
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) putSubtitleStyle(w http.ResponseWriter, r *http.Request) {
	var req dv.SaveStyleRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.SaveSubtitleStyle(r.Context(), humanActor, r.PathValue("run"), req)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) getSubtitleFonts(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.SubtitleFonts(r.Context())
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
