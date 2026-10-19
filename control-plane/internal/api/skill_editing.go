package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (s *Server) skillEditRoutes() {
	s.mux.HandleFunc("POST /api/human-session", s.humanSkillSession)
	s.mux.HandleFunc("DELETE /api/human-session", s.humanSkillLogout)
	s.mux.HandleFunc("GET /api/agent-operations", s.agentOperations)
	s.mux.HandleFunc("GET /api/agent-operations/verify", s.verifyAgentOperations)
	s.mux.HandleFunc("POST /api/agent-operations/notes", s.agentOperationNote)
	s.mux.HandleFunc("GET /api/skill-runs/{id}/workbench", s.skillWorkbench)
	s.mux.HandleFunc("PUT /api/skill-runs/{id}/plan", s.skillSavePlan)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/edits", s.skillRequestEdit)
	s.mux.HandleFunc("GET /api/skill-runs/{id}/checkpoints", s.skillCheckpoints)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/undo", s.skillHistoryMove)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/redo", s.skillHistoryMove)
	s.mux.HandleFunc("GET /api/skill-runs/{id}/checkpoints/{checkpoint}/artifacts/{key}", s.skillHistoryPreview)
}
func (s *Server) agentOperations(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, e := s.svc.AgentOperations(r.Context(), humanActor, r.URL.Query().Get("run_id"), after, limit)
	skillResponse(w, rows, e)
}
func (s *Server) verifyAgentOperations(w http.ResponseWriter, r *http.Request) {
	sequence, e := s.svc.VerifyAgentOperations(r.Context(), humanActor)
	skillResponse(w, map[string]any{"verified_through": sequence}, e)
}
func (s *Server) agentOperationNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunID string `json:"run_id"`
		Note  string `json:"note"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	if strings.TrimSpace(req.Note) == "" || len(req.Note) > 8192 {
		writeError(w, 400, "invalid_argument", "invalid note")
		return
	}
	raw := []byte(strconv.Quote(req.Note))
	e := s.svc.RecordAgentOperation(r.Context(), humanActor, req.RunID, "human.note", raw)
	skillResponse(w, map[string]bool{"appended": e == nil}, e)
}
func (s *Server) skillWorkbench(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.SkillWorkbench(r.Context(), humanActor, r.PathValue("id"))
	if e == nil {
		for _, preview := range v["previews"].([]map[string]any) {
			if preview["available"] != true {
				continue
			}
			artifact := preview["artifact"].(skill.Artifact)
			path := "/api/skill-runs/" + url.PathEscape(r.PathValue("id")) + "/artifacts/" + url.PathEscape(artifact.ID)
			if cp, _ := preview["checkpoint_id"].(string); cp != "" {
				path = "/api/skill-runs/" + url.PathEscape(r.PathValue("id")) + "/checkpoints/" + url.PathEscape(cp) + "/artifacts/" + url.PathEscape(artifact.ID)
			}
			preview["preview_url"], e = issueHumanMediaTicket(path)
			if e != nil {
				break
			}
		}
	}
	skillResponse(w, v, e)
}
func (s *Server) skillSavePlan(w http.ResponseWriter, r *http.Request) {
	var req service.SkillPlanSave
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.SaveSkillPlan(r.Context(), humanActor, r.PathValue("id"), req)
	skillResponse(w, v, e)
}
func (s *Server) skillRequestEdit(w http.ResponseWriter, r *http.Request) {
	var req service.SkillEditRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.RequestSkillEdit(r.Context(), humanActor, r.PathValue("id"), req)
	skillResponse(w, v, e)
}
func (s *Server) skillCheckpoints(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.SkillEditHistory(r.Context(), humanActor, r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillHistoryMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version int `json:"expected_version"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	v, e := s.svc.MoveSkillHistory(r.Context(), humanActor, r.PathValue("id"), action, req.Version)
	skillResponse(w, v, e)
}
func (s *Server) skillHistoryPreview(w http.ResponseWriter, r *http.Request) {
	path, a, e := s.svc.SkillCheckpointPreview(r.Context(), humanActor, r.PathValue("id"), r.PathValue("checkpoint"), r.PathValue("key"))
	if e != nil {
		writeServiceError(w, e)
		return
	}
	w.Header().Set("X-Content-SHA256", a.SHA)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFile(w, r, path)
}
