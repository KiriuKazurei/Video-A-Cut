package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

func validPreviewID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\")
}

// POST /api/workflows/{run}/previews
func (s *Server) startPreview(w http.ResponseWriter, r *http.Request) {
	run := r.PathValue("run")
	if !validPreviewID(run) {
		writeServiceError(w, fmt.Errorf("invalid run id %q: %w", run, model.ErrArgument))
		return
	}
	var req preview.StartRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	view, created, err := s.svc.StartPreview(r.Context(), humanActor, run, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	writeJSON(w, status, view)
}

// GET /api/workflows/{run}/previews
func (s *Server) listPreviews(w http.ResponseWriter, r *http.Request) {
	run := r.PathValue("run")
	if !validPreviewID(run) {
		writeServiceError(w, fmt.Errorf("invalid run id %q: %w", run, model.ErrArgument))
		return
	}
	list, err := s.svc.ListPreviews(r.Context(), run)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/previews/{id}
func (s *Server) getPreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validPreviewID(id) {
		writeServiceError(w, fmt.Errorf("invalid preview id %q: %w", id, model.ErrArgument))
		return
	}
	view, err := s.svc.GetPreview(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// POST /api/previews/{id}/cancel
func (s *Server) cancelPreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validPreviewID(id) {
		writeServiceError(w, fmt.Errorf("invalid preview id %q: %w", id, model.ErrArgument))
		return
	}
	view, err := s.svc.CancelPreview(r.Context(), humanActor, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// GET /api/previews/{id}/files/{key}
func (s *Server) getPreviewFile(w http.ResponseWriter, r *http.Request) {
	id, key := r.PathValue("id"), r.PathValue("key")
	if !validPreviewID(id) || !validPreviewID(key) {
		writeServiceError(w, fmt.Errorf("invalid preview file address: %w", model.ErrArgument))
		return
	}
	file, err := s.svc.ResolvePreviewFile(r.Context(), id, key)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	f, err := os.Open(file.AbsolutePath)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeServiceError(w, err)
		return
	}

	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Type", file.MIME)

	if key != "mp4" || r.URL.Query().Get("download") == "1" {
		h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Name))
	}

	http.ServeContent(w, r, file.Name, info.ModTime(), f)
}
