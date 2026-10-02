package api

import (
	"archive/zip"
	"io"
	"net/http"
	"os"
	"sort"
)

func (s *Server) workflowEvidence(w http.ResponseWriter, r *http.Request) {
	path, err := s.svc.WorkflowEvidencePath(r.Context(), r.PathValue("run"), r.URL.Query().Get("revision_id"), r.PathValue("key"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	f, err := os.Open(path)
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
func (s *Server) workflowZIP(w http.ResponseWriter, r *http.Request) {
	files, err := s.svc.WorkflowPackageFiles(r.Context(), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	opened := map[string]*os.File{}
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	names := []string{}
	for name, path := range files {
		f, err := os.Open(path)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		opened[name] = f
		names = append(names, name)
	}
	sort.Strings(names)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=workflow-delivery.zip")
	w.Header().Set("Cache-Control", "private, no-store")
	archive := zip.NewWriter(w)
	defer archive.Close()
	for _, name := range names {
		entry, err := archive.Create(name)
		if err != nil {
			return
		}
		if _, err := io.Copy(entry, opened[name]); err != nil {
			return
		}
	}
	note, err := archive.Create("README-RELINK.txt")
	if err == nil {
		io.WriteString(note, "Import edit.xml and subtitles.srt into Premiere. If media is offline after moving the ZIP, use Link Media and select the extracted media directory. This package is awaiting human content and sync acceptance.\n")
	}
}
