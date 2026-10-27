package api

import "net/http"

import "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"

func (s *Server) drawRoutes() {
	s.mux.HandleFunc("GET /api/projects/{id}/draw-compositions/{composition}/history", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.DrawingHistory(r.PathValue("id"), r.PathValue("composition"))
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/draw-compositions/{composition}/history", func(w http.ResponseWriter, r *http.Request) {
		var req service.DrawHistoryRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.MoveDrawingHistory(r.Context(), r.PathValue("id"), r.PathValue("composition"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/draw-capabilities", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.DrawCapabilities(r.PathValue("id"))
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/draw-compositions/{composition}/preview", func(w http.ResponseWriter, r *http.Request) {
		var req service.DrawPreviewRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.PreviewDrawComposition(r.Context(), r.PathValue("id"), r.PathValue("composition"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("DELETE /api/projects/{id}/draw-compositions/{composition}/preview", func(w http.ResponseWriter, r *http.Request) {
		e := s.svc.CloseDrawPreview(r.PathValue("id"), r.URL.Query().Get("session"), r.PathValue("composition"))
		skillResponse(w, map[string]bool{"closed": e == nil}, e)
	})
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
		var req service.DrawRenderRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.RenderDrawCompositionFormat(r.Context(), r.PathValue("id"), r.PathValue("composition"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/draw-compositions/{composition}/render", func(w http.ResponseWriter, r *http.Request) {
		path, _, e := s.svc.DrawRenderPathFormat(r.PathValue("id"), r.PathValue("composition"), r.URL.Query().Get("version"), r.URL.Query().Get("format"))
		if e != nil {
			writeServiceError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/draw-compositions/{composition}/export", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			service.DrawRenderRequest
			Path string `json:"path"`
		}
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ExportDrawComposition(r.Context(), r.PathValue("id"), r.PathValue("composition"), req.DrawRenderRequest, req.Path)
		skillResponse(w, v, e)
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
