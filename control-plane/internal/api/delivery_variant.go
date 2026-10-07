package api

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func validDeliveryVariantID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\")
}

func (s *Server) getDeliveryVariantZIP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeliveryVariantID(id) {
		writeServiceError(w, model.ErrArgument)
		return
	}
	files, err := s.svc.DeliveryVariantZIPFiles(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", id+"-ass.zip"))
	writer := zip.NewWriter(w)
	for _, file := range files {
		if r.Context().Err() != nil {
			_ = writer.Close()
			return
		}
		input, e := os.Open(file.AbsolutePath)
		if e != nil {
			panic(http.ErrAbortHandler)
		}
		entry, e := writer.Create(file.Name)
		if e != nil {
			input.Close()
			panic(http.ErrAbortHandler)
		}
		_, e = io.Copy(entry, input)
		input.Close()
		if e != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if writer.Close() != nil {
		panic(http.ErrAbortHandler)
	}
}

// POST /api/workflows/{run}/delivery-variants
func (s *Server) startDeliveryVariant(w http.ResponseWriter, r *http.Request) {
	run := r.PathValue("run")
	if !validDeliveryVariantID(run) {
		writeServiceError(w, fmt.Errorf("invalid run id %q: %w", run, model.ErrArgument))
		return
	}
	var req deliveryvariant.StartRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	view, created, err := s.svc.StartDeliveryVariant(r.Context(), humanActor, run, req)
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

// GET /api/workflows/{run}/delivery-variants
func (s *Server) listDeliveryVariants(w http.ResponseWriter, r *http.Request) {
	run := r.PathValue("run")
	if !validDeliveryVariantID(run) {
		writeServiceError(w, fmt.Errorf("invalid run id %q: %w", run, model.ErrArgument))
		return
	}
	list, err := s.svc.ListDeliveryVariants(r.Context(), run)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/delivery-variants/{id}
func (s *Server) getDeliveryVariant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeliveryVariantID(id) {
		writeServiceError(w, fmt.Errorf("invalid deliveryVariant id %q: %w", id, model.ErrArgument))
		return
	}
	view, err := s.svc.GetDeliveryVariant(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// POST /api/delivery-variants/{id}/cancel
func (s *Server) cancelDeliveryVariant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeliveryVariantID(id) {
		writeServiceError(w, fmt.Errorf("invalid deliveryVariant id %q: %w", id, model.ErrArgument))
		return
	}
	view, err := s.svc.CancelDeliveryVariant(r.Context(), humanActor, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// GET /api/delivery-variants/{id}/files/{key}
func (s *Server) getDeliveryVariantFile(w http.ResponseWriter, r *http.Request) {
	id, key := r.PathValue("id"), r.PathValue("key")
	if !validDeliveryVariantID(id) || !validDeliveryVariantID(key) {
		writeServiceError(w, fmt.Errorf("invalid deliveryVariant file address: %w", model.ErrArgument))
		return
	}
	file, err := s.svc.ResolveDeliveryVariantFile(r.Context(), id, key)
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
