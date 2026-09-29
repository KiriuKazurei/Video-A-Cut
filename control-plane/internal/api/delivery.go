package api

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

type importDeliveryRequest struct {
	AssetID    string `json:"asset_id"`
	PackageDir string `json:"package_dir"`
}

func validDeliveryID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\")
}

func (s *Server) importDelivery(w http.ResponseWriter, r *http.Request) {
	var req importDeliveryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	if !validDeliveryID(req.AssetID) || req.PackageDir == "" {
		writeServiceError(w, fmt.Errorf("asset_id and package_dir are required: %w", model.ErrArgument))
		return
	}
	asset, err := s.svc.ImportDelivery(r.Context(), req.AssetID, req.PackageDir)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

// reopenAsset answers POST /api/assets/{id}/reopen: a human takes an exported
// package back to ingested so the pipeline can run another round on it.
func (s *Server) reopenAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeliveryID(id) {
		writeServiceError(w, fmt.Errorf("invalid asset id: %w", model.ErrArgument))
		return
	}
	asset, err := s.svc.ReopenAsset(r.Context(), humanActor, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) listDeliveryFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeliveryID(id) {
		writeServiceError(w, fmt.Errorf("invalid asset id: %w", model.ErrArgument))
		return
	}
	files, err := s.svc.ListDeliveryFiles(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) getDeliveryFile(w http.ResponseWriter, r *http.Request) {
	id, key := r.PathValue("id"), r.PathValue("key")
	if !validDeliveryID(id) || !validDeliveryID(key) {
		writeServiceError(w, fmt.Errorf("invalid delivery file address: %w", model.ErrArgument))
		return
	}
	f, file, err := s.svc.OpenDeliveryFile(r.Context(), id, key)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Type", file.Mime)
	// Only known media types render inline. XML/JSON/subtitles are attachments,
	// even when a browser would otherwise try to interpret them as a page.
	if !file.Playable || r.URL.Query().Get("download") == "1" {
		h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", key+filepath.Ext(file.Name)))
	}
	info, err := f.Stat()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	http.ServeContent(w, r, file.Name, info.ModTime(), f)
}

func (s *Server) downloadDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeliveryID(id) {
		writeServiceError(w, fmt.Errorf("invalid asset id: %w", model.ErrArgument))
		return
	}
	files, err := s.svc.ListDeliveryFiles(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if len(files) == 0 {
		writeServiceError(w, fmt.Errorf("asset has no delivery files: %w", model.ErrInvalidState))
		return
	}
	// Open all entries before sending 200. A missing file must answer with a
	// JSON error, not a successful but silently incomplete archive.
	type entry struct {
		file *os.File
		meta service.DeliveryFile
	}
	opened := make([]entry, 0, len(files))
	defer func() {
		for _, item := range opened {
			_ = item.file.Close()
		}
	}()
	for _, file := range files {
		f, meta, err := s.svc.OpenDeliveryFile(r.Context(), id, file.Key)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		opened = append(opened, entry{file: f, meta: meta})
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", id+"-delivery.zip"))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	archive := zip.NewWriter(w)
	defer archive.Close()
	for _, item := range opened {
		if r.Context().Err() != nil {
			return
		}
		entry, err := archive.Create(item.meta.PackagePath)
		if err == nil {
			_, err = io.Copy(entry, item.file)
		}
		if err != nil {
			return
		}
	}
	// CLI XML contains absolute file:// paths for reliable same-machine import.
	// Moving the ZIP to another directory or machine can therefore require
	// Premiere's built-in Link Media workflow. Ship that instruction with the
	// media rather than implying that a downloaded ZIP is path-independent.
	hasNote := false
	for _, item := range opened {
		if item.meta.PackagePath == "README-RELINK.txt" {
			hasNote = true
			break
		}
	}
	if !hasNote {
		entry, err := archive.Create("README-RELINK.txt")
		if err == nil {
			_, _ = io.WriteString(entry, "Video Auto Cut delivery package\n\nThe included Premiere XML may contain absolute file paths from the machine that produced it. After extracting this ZIP elsewhere, if Premiere reports offline media, use File > Link Media, locate the matching file inside the extracted media directory, and enable automatic relinking of other files. Keep the directory structure unchanged.\n")
		}
	}
}
