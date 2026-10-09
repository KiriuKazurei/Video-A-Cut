package api

import "net/http"

func (s *Server) runtimeActivity(w http.ResponseWriter, r *http.Request) {
	count, err := s.svc.RuntimeActivity(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "cannot read active tasks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"active_tasks": count})
}
