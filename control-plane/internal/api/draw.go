package api

import "net/http"

import "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"

func (s *Server) drawRoutes() {
	s.mux.HandleFunc("GET /api/projects/{id}/draw-compositions", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.DrawCompositions(r.PathValue("id"))
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/draw-compositions", func(w http.ResponseWriter, r *http.Request) {
		var req service.DrawSaveRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.SaveDrawComposition(r.Context(), r.PathValue("id"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/draw-compositions/{composition}/check", func(w http.ResponseWriter, r *http.Request) {
		var req service.DrawActionRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.CheckDrawComposition(r.Context(), r.PathValue("id"), r.PathValue("composition"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/draw-compositions/{composition}/render", func(w http.ResponseWriter, r *http.Request) {
		var req service.DrawActionRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.RenderDrawComposition(r.Context(), r.PathValue("id"), r.PathValue("composition"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/draw-compositions/{composition}/render", func(w http.ResponseWriter, r *http.Request) {
		path, _, e := s.svc.DrawRenderPath(r.PathValue("id"), r.PathValue("composition"), r.URL.Query().Get("version"))
		if e != nil {
			writeServiceError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/draw-compositions/{composition}/import", func(w http.ResponseWriter, r *http.Request) {
		var req service.DrawImportRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ImportDrawComposition(r.Context(), "human:window", r.PathValue("id"), r.PathValue("composition"), req)
		skillResponse(w, v, e)
	})
}
