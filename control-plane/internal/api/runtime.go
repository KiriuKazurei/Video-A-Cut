package api

import "net/http"

func (s *Server) systemDiagnostics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	report, err := s.svc.SystemDiagnostics(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "diagnostic_incomplete", "本机诊断未完成，请稍后重试")
		return
	}
	if r.URL.Path == "/api/system/diagnostics/report" {
		w.Header().Set("Content-Disposition", `attachment; filename="vac-diagnostics.json"`)
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) runtimeActivity(w http.ResponseWriter, r *http.Request) {
	count, err := s.svc.RuntimeActivity(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "cannot read active tasks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"active_tasks": count})
}
