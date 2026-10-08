package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
	"net/http"
	"os"
	"strconv"
)

func (s *Server) ttsRoutes() {
	s.mux.HandleFunc("GET /api/tts/capabilities", s.ttsCapabilities)
	s.mux.HandleFunc("GET /api/tts/templates", s.ttsTemplates)
	s.mux.HandleFunc("POST /api/tts/templates", s.ttsSaveTemplate)
	s.mux.HandleFunc("GET /api/tts/templates/{id}/revisions/{revision}", s.ttsTemplate)
	s.mux.HandleFunc("POST /api/tts/templates/{id}/revisions", s.ttsSaveTemplate)
	s.mux.HandleFunc("POST /api/tts/templates/{id}/archive", s.ttsArchiveTemplate)
	s.mux.HandleFunc("GET /api/tts/preview-jobs", s.ttsPreviews)
	s.mux.HandleFunc("POST /api/tts/preview-jobs", s.ttsStartPreview)
	s.mux.HandleFunc("GET /api/tts/preview-jobs/{id}", s.ttsPreview)
	s.mux.HandleFunc("POST /api/tts/preview-jobs/{id}/cancel", s.ttsCancelPreview)
	s.mux.HandleFunc("GET /api/tts/preview-jobs/{id}/audio", s.ttsAudio)
}
func (s *Server) ttsCapabilities(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.TTSCapabilities(r.Context(), r.URL.Query().Get("token_env"))
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) ttsTemplates(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.ListTTSTemplates(r.Context())
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) ttsSaveTemplate(w http.ResponseWriter, r *http.Request) {
	var req tts.SaveRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	if id := r.PathValue("id"); id != "" && id != req.TemplateID {
		writeServiceError(w, model.ErrArgument)
		return
	}
	v, e := s.svc.SaveTTSTemplate(r.Context(), humanActor, req)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (s *Server) ttsTemplate(w http.ResponseWriter, r *http.Request) {
	n, e := strconv.Atoi(r.PathValue("revision"))
	if e != nil || n < 0 {
		writeServiceError(w, model.ErrArgument)
		return
	}
	v, e := s.svc.GetTTSTemplate(r.Context(), r.PathValue("id"), n)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) ttsArchiveTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ExpectedRevision int `json:"expected_revision"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	if e := s.svc.ArchiveTTSTemplate(r.Context(), humanActor, r.PathValue("id"), req.ExpectedRevision); e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"archived": true})
}
func (s *Server) ttsStartPreview(w http.ResponseWriter, r *http.Request) {
	var req tts.PreviewRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.StartTTSPreview(r.Context(), humanActor, req)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 202, v)
}
func (s *Server) ttsPreviews(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.ListTTSPreviews(r.Context())
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) ttsPreview(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.GetTTSPreview(r.Context(), r.PathValue("id"))
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) ttsCancelPreview(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.CancelTTSPreview(r.Context(), humanActor, r.PathValue("id"))
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) ttsAudio(w http.ResponseWriter, r *http.Request) {
	path, e := s.svc.TTSPreviewAudio(r.Context(), r.PathValue("id"))
	if e != nil {
		writeServiceError(w, e)
		return
	}
	f, e := os.Open(path)
	if e != nil {
		writeServiceError(w, model.ErrNotFound)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		writeServiceError(w, e)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "private, no-store")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="tts-preview.wav"`)
	}
	http.ServeContent(w, r, "tts-preview.wav", info.ModTime(), f)
}
