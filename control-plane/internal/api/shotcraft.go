package api

import (
	"net/http"
	"strconv"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/shotcraft"
)

func (s *Server) shotcraftRoutes() {
	s.mux.HandleFunc("GET /api/shotcraft/directory", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.svc.ShotTemplateDirectory(r.Context())
		skillResponse(w, value, err)
	})
	s.mux.HandleFunc("PUT /api/shotcraft/directory", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Directory *string `json:"directory"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeServiceError(w, err)
			return
		}
		if req.Directory == nil {
			writeServiceError(w, model.ErrArgument)
			return
		}
		value, err := s.svc.SetShotTemplateDirectory(r.Context(), service.ShotTemplateDirectory{Directory: *req.Directory})
		skillResponse(w, value, err)
	})
	s.mux.HandleFunc("GET /api/shotcraft/templates", func(w http.ResponseWriter, r *http.Request) {
		cards, err := s.svc.ListShotCards(r.Context(), r.URL.Query().Get("query"))
		skillResponse(w, cards, err)
	})
	s.mux.HandleFunc("POST /api/shotcraft/templates", func(w http.ResponseWriter, r *http.Request) {
		var entry shotcraft.UserTemplate
		if err := decodeJSON(w, r, &entry); err != nil {
			writeServiceError(w, err)
			return
		}
		card, err := s.svc.ImportShotTemplate(r.Context(), entry)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, card)
	})
	s.mux.HandleFunc("GET /api/shotcraft/templates/{card}", func(w http.ResponseWriter, r *http.Request) {
		entry, err := s.svc.UserShotTemplate(r.Context(), r.PathValue("card"))
		skillResponse(w, entry, err)
	})
	s.mux.HandleFunc("DELETE /api/shotcraft/templates/{card}", func(w http.ResponseWriter, r *http.Request) {
		err := s.svc.DeleteShotTemplate(r.Context(), r.PathValue("card"))
		skillResponse(w, map[string]bool{"deleted": err == nil}, err)
	})
	s.mux.HandleFunc("GET /api/shotcraft/templates/{card}/preview", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		position := 50
		if raw := query.Get("position"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				writeServiceError(w, model.ErrArgument)
				return
			}
			position = value
		}
		reference := r.PathValue("card")
		if variant := query.Get("variant"); variant != "" {
			reference += "/" + variant
		}
		png, err := s.svc.PreviewShotTemplate(r.Context(), reference, position)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})
}
