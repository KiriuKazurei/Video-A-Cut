package api

import (
	"archive/zip"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func (s *Server) scriptWorkbenchRoutes() {
	s.mux.HandleFunc("POST /api/script-workbench/parse", s.parseScript)
	s.mux.HandleFunc("GET /api/script-workbench/media", s.scriptMedia)
	s.mux.HandleFunc("GET /api/script-workbench/fonts", s.scriptFonts)
	s.mux.HandleFunc("GET /api/scripts/{id}/workbench", s.scriptWorkbench)
	s.mux.HandleFunc("PUT /api/scripts/{id}/workbench", s.saveScriptWorkbench)
	s.mux.HandleFunc("PUT /api/scripts/{id}/voice", s.saveScriptVoice)
	s.mux.HandleFunc("GET /api/scripts/{id}/revisions", s.scriptRevisions)
	s.mux.HandleFunc("POST /api/scripts/{id}/generate", s.generateScript)
	s.mux.HandleFunc("POST /api/scripts/{id}/reject", s.rejectScript)
	s.mux.HandleFunc("POST /api/scripts/{id}/workbench/finalize", s.finalizeScriptWorkbench)
	s.mux.HandleFunc("POST /api/scripts/{id}/export", s.exportScript)
	s.mux.HandleFunc("GET /api/scripts/{id}/delivery", s.scriptDeliveryInfo)
	s.mux.HandleFunc("GET /api/scripts/{id}/preview.mp4", s.scriptPreview)
	s.mux.HandleFunc("GET /api/scripts/{id}/delivery.zip", s.scriptZIP)
}

func (s *Server) saveScriptVoice(w http.ResponseWriter, r *http.Request) {
	var req service.SaveScriptVoiceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	v, err := s.svc.SaveScriptVoice(r.Context(), humanActor, r.PathValue("id"), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) parseScript(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scriptsyntax.Parse(req.Body))
}
func (s *Server) scriptMedia(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.ScriptMediaCatalogue(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) scriptFonts(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.ScriptFonts(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) scriptWorkbench(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.GetScriptWorkbench(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) saveScriptWorkbench(w http.ResponseWriter, r *http.Request) {
	var req service.SaveScriptRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	v, err := s.svc.SaveScriptWorkbench(r.Context(), humanActor, r.PathValue("id"), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) scriptRevisions(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.ScriptRevisions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) generateScript(w http.ResponseWriter, r *http.Request) {
	var req service.GenerateScriptRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	v, err := s.svc.GenerateScriptWorkbench(r.Context(), humanActor, r.PathValue("id"), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type scriptVersionRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	VisibleSetSHA   string `json:"visible_set_sha"`
}

func (s *Server) rejectScript(w http.ResponseWriter, r *http.Request) {
	var req scriptVersionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	v, err := s.svc.RejectScriptWorkbench(r.Context(), humanActor, r.PathValue("id"), req.ExpectedVersion, req.VisibleSetSHA)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) finalizeScriptWorkbench(w http.ResponseWriter, r *http.Request) {
	var req scriptVersionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	v, err := s.svc.FinalizeScriptWorkbench(r.Context(), humanActor, r.PathValue("id"), req.ExpectedVersion, req.VisibleSetSHA)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) exportScript(w http.ResponseWriter, r *http.Request) {
	var req scriptVersionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	v, err := s.svc.ExportScriptWorkbench(r.Context(), humanActor, r.PathValue("id"), req.ExpectedVersion, req.VisibleSetSHA)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) scriptPreview(w http.ResponseWriter, r *http.Request) {
	version, err := scriptRequestedVersion(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	p, err := s.svc.ScriptDownloadPath(r.Context(), r.PathValue("id"), "final.mp4", version, r.URL.Query().Get("sha256"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, p)
}
func (s *Server) scriptZIP(w http.ResponseWriter, r *http.Request) {
	version, err := scriptRequestedVersion(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	root, err := s.svc.ScriptDownloadPath(r.Context(), r.PathValue("id"), "", version, r.URL.Query().Get("sha256"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	files := []string{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return model.ErrForbidden
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return model.ErrForbidden
		}
		files = append(files, p)
		if len(files) > 500 {
			return model.ErrArgument
		}
		return nil
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="script-delivery.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, p := range files {
		rel, err := filepath.Rel(root, p)
		if err != nil || !filepath.IsLocal(rel) {
			return
		}
		f, err := os.Open(p)
		if err != nil {
			return
		}
		entry, err := zw.Create(filepath.ToSlash(rel))
		if err == nil {
			_, err = io.Copy(entry, f)
		}
		f.Close()
		if err != nil {
			return
		}
	}
}

func scriptRequestedVersion(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("version")
	if raw == "" {
		return 0, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 {
		return 0, model.ErrArgument
	}
	return version, nil
}
func (s *Server) scriptDeliveryInfo(w http.ResponseWriter, r *http.Request) {
	version, err := scriptRequestedVersion(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	info, err := s.svc.ScriptDeliveryInfo(r.Context(), r.PathValue("id"), version)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, info)
}
