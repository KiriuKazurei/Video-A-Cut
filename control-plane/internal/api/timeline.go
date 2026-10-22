package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"net/http"
	"strconv"
)

func (s *Server) timelineRoutes() {
	s.sequenceMediaRoutes()
	s.mux.HandleFunc("POST /api/projects/{id}/sequences", func(w http.ResponseWriter, r *http.Request) {
		var req service.SequenceRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeServiceError(w, err)
			return
		}
		v, e := s.svc.ManageSequence(r.Context(), r.PathValue("id"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/sequences/{sequence}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.Timeline(r.Context(), r.PathValue("id"), r.PathValue("sequence"))
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/sequences/{sequence}/history", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.Timeline(r.Context(), r.PathValue("id"), r.PathValue("sequence"))
		skillResponse(w, v.History, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/sequences/{sequence}/commands", func(w http.ResponseWriter, r *http.Request) {
		var req project.Command
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.TimelineCommand(r.Context(), "human:window", r.PathValue("id"), r.PathValue("sequence"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/references", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.ReferenceDirectory(r.PathValue("id"), r.URL.Query().Get("sequence"))
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/references/resolve", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			References []project.Reference `json:"references"`
		}
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ResolveProjectReferences(r.PathValue("id"), req.References)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/sequences/{sequence}/frame-modifications", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.svc.Timeline(r.Context(), r.PathValue("id"), r.PathValue("sequence")); err != nil {
			writeServiceError(w, err)
			return
		}
		writeError(w, http.StatusNotImplemented, "not_implemented", "画面修改能力尚未启用")
	})
}

func (s *Server) sequenceMediaRoutes() {
	s.mux.HandleFunc("POST /api/projects/{id}/sequences/{sequence}/skill-results", func(w http.ResponseWriter, r *http.Request) {
		var req service.SequenceResultRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ImportSequenceSkillResult(r.Context(), r.PathValue("id"), r.PathValue("sequence"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/sequences/{sequence}/preview", func(w http.ResponseWriter, r *http.Request) {
		var req service.SequencePreviewRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.CreateSequencePreview(r.Context(), r.PathValue("id"), r.PathValue("sequence"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/sequences/{sequence}/export", func(w http.ResponseWriter, r *http.Request) {
		var req service.SequencePreviewRequest
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ExportSequence(r.Context(), r.PathValue("id"), r.PathValue("sequence"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/sequences/{sequence}/preview", func(w http.ResponseWriter, r *http.Request) {
		path, e := s.svc.SequencePreviewPath(r.Context(), r.PathValue("id"), r.PathValue("sequence"), r.URL.Query().Get("version"))
		if e != nil {
			writeServiceError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/resources/{resource}/waveform", func(w http.ResponseWriter, r *http.Request) {
		index, e := strconv.Atoi(r.URL.Query().Get("stream"))
		if e != nil {
			writeError(w, 400, "invalid_stream", "请选择音频流")
			return
		}
		v, e := s.svc.ResourceWaveform(r.Context(), r.PathValue("id"), r.PathValue("resource"), index)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/resources/{resource}/loudness", func(w http.ResponseWriter, r *http.Request) {
		index, e := strconv.Atoi(r.URL.Query().Get("stream"))
		if e != nil || index < 0 {
			writeError(w, 400, "invalid_stream", "请选择音频流")
			return
		}
		v, e := s.svc.ResourceLoudness(r.Context(), r.PathValue("id"), r.PathValue("resource"), index)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/resources/{resource}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		at, e := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
		if e != nil {
			writeError(w, 400, "invalid_time", "请选择源帧位置")
			return
		}
		path, e := s.svc.ResourceThumbnail(r.Context(), r.PathValue("id"), r.PathValue("resource"), at)
		if e != nil {
			writeServiceError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, path)
	})
}
