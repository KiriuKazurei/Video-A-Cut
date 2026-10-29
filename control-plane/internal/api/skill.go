package api

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"net/http"
	"strings"
)

func (s *Server) skillRoutes() {
	s.projectRoutes()
	s.skillEditRoutes()
	s.mux.HandleFunc("GET /api/skill-runs/{id}/revisions", s.skillRevisions)
	s.mux.HandleFunc("PUT /api/skill-runs/{id}/voice", s.skillVoice)
	s.mux.HandleFunc("POST /api/projects/{id}/media", s.skillImportMedia)
	s.mux.HandleFunc("GET /api/projects", s.skillProjects)
	s.mux.HandleFunc("POST /api/projects", s.skillSaveProject)
	s.mux.HandleFunc("GET /api/projects/{id}", s.skillProject)
	s.mux.HandleFunc("PUT /api/projects/{id}", s.skillSaveProject)
	s.mux.HandleFunc("GET /api/projects/{id}/visible-set", s.skillVisible)
	s.mux.HandleFunc("POST /api/projects/{id}/visible-set/confirm", s.skillConfirmVisible)
	s.mux.HandleFunc("POST /api/projects/{id}/exports/ffmpeg", s.skillExport)
	s.mux.HandleFunc("GET /api/skills", s.skillList)
	s.mux.HandleFunc("GET /api/skills/online/capabilities", s.onlineSkillCapabilities)
	s.mux.HandleFunc("POST /api/skills/online/install", s.onlineSkillReserved)
	s.mux.HandleFunc("POST /api/skills/{id}/check-updates", s.onlineSkillReserved)
	s.mux.HandleFunc("POST /api/skills/{id}/update", s.onlineSkillReserved)
	s.mux.HandleFunc("POST /api/skills", s.skillImport)
	s.mux.HandleFunc("GET /api/skills/{id}", s.skillGet)
	s.mux.HandleFunc("PATCH /api/skills/{id}", s.skillEnable)
	s.mux.HandleFunc("GET /api/skills/{id}/content", s.skillGet)
	s.mux.HandleFunc("POST /api/skills/{id}/validate", s.skillValidate)
	s.mux.HandleFunc("GET /api/system/ffmpeg", s.skillFFmpeg)
	s.mux.HandleFunc("GET /api/skill-runs", s.skillRuns)
	s.mux.HandleFunc("POST /api/skill-runs", s.skillCreateRun)
	s.mux.HandleFunc("GET /api/skill-runs/{id}", s.skillRun)
	s.mux.HandleFunc("GET /api/skill-runs/{id}/context", s.skillContext)
	s.mux.HandleFunc("GET /api/skill-runs/{id}/steps", s.skillSteps)
	s.mux.HandleFunc("GET /api/skill-runs/{id}/artifacts/{key}", s.skillArtifact)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/tools", s.skillTool)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/finalize", s.skillAction)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/resume", s.skillAction)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/cancel", s.skillAction)
	s.mux.HandleFunc("POST /api/skill-runs/{id}/execute", s.skillExecute)
}

func (s *Server) onlineSkillCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "enabled": false, "status": "reserved", "operations": []string{"install", "check_updates", "update"}})
}
func (s *Server) onlineSkillReserved(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "feature_reserved", "在线 Skill 安装与更新仅预留接口，当前尚未开放")
}
func (s *Server) skillRevisions(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.SkillRunRevisions(r.Context(), humanActor, r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillVoice(w http.ResponseWriter, r *http.Request) {
	var req service.SkillVoiceRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.SaveSkillVoice(r.Context(), humanActor, r.PathValue("id"), req)
	skillResponse(w, v, e)
}
func (s *Server) skillImportMedia(w http.ResponseWriter, r *http.Request) {
	var req service.ProjectImportRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.ImportProjectMedia(r.Context(), humanActor, r.PathValue("id"), req)
	skillResponse(w, v, e)
}
func skillResponse(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *Server) skillProjects(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.ListProjects(r.Context())
	skillResponse(w, v, e)
}
func (s *Server) skillProject(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.GetProject(r.Context(), r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillSaveProject(w http.ResponseWriter, r *http.Request) {
	var req service.ProjectRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.SaveProject(r.Context(), humanActor, r.PathValue("id"), req)
	skillResponse(w, v, e)
}
func (s *Server) skillVisible(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.ProjectVisibleSet(r.Context(), r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillConfirmVisible(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SHA     string `json:"visible_set_sha"`
		Version int    `json:"expected_version"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.ConfirmProjectVisible(r.Context(), humanActor, r.PathValue("id"), req.SHA, req.Version)
	skillResponse(w, v, e)
}
func (s *Server) skillList(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.ListSkills(r.Context())
	skillResponse(w, v, e)
}
func (s *Server) skillGet(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.GetSkill(r.Context(), r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.ImportSkill(r.Context(), humanActor, req.Path)
	skillResponse(w, v, e)
}
func (s *Server) skillEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.EnableSkill(r.Context(), humanActor, r.PathValue("id"), req.Enabled)
	skillResponse(w, v, e)
}
func (s *Server) skillValidate(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.ValidateSkill(r.Context(), r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillFFmpeg(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.FFmpegCapabilities(r.Context())
	skillResponse(w, v, e)
}
func (s *Server) skillRuns(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.ListSkillRuns(r.Context(), humanActor, r.URL.Query().Get("project_id"))
	skillResponse(w, v, e)
}
func (s *Server) skillCreateRun(w http.ResponseWriter, r *http.Request) {
	var req skill.RunRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.CreateSkillRun(r.Context(), humanActor, req)
	skillResponse(w, v, e)
}
func (s *Server) skillRun(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.GetSkillRun(r.Context(), humanActor, r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillContext(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.SkillRunContext(r.Context(), humanActor, r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillSteps(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.SkillRunSteps(r.Context(), humanActor, r.PathValue("id"))
	skillResponse(w, v, e)
}
func (s *Server) skillTool(w http.ResponseWriter, r *http.Request) {
	var req skill.ToolRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	run, result, e := s.svc.ExecuteSkillTool(r.Context(), humanActor, r.PathValue("id"), req)
	skillResponse(w, map[string]any{"run": run, "result": result}, e)
}
func (s *Server) skillAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version int `json:"expected_version"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	v, e := s.svc.UpdateSkillRun(r.Context(), humanActor, r.PathValue("id"), action, req.Version)
	skillResponse(w, v, e)
}
func (s *Server) skillExecute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version int `json:"expected_version"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := s.svc.RunBuiltinAgent(r.Context(), humanActor, r.PathValue("id"), req.Version)
	skillResponse(w, v, e)
}
func (s *Server) skillArtifact(w http.ResponseWriter, r *http.Request) {
	path, a, e := s.svc.SkillArtifactPath(r.Context(), humanActor, r.PathValue("id"), r.PathValue("key"))
	if e != nil {
		writeServiceError(w, e)
		return
	}
	w.Header().Set("X-Content-SHA256", a.SHA)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFile(w, r, path)
}
func (s *Server) skillExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunID   string `json:"run_id"`
		Version int    `json:"expected_version"`
		Key     string `json:"idempotency_key"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	run, e := s.svc.GetSkillRun(r.Context(), humanActor, req.RunID)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	if run.ProjectID != r.PathValue("id") {
		writeServiceError(w, model.ErrForbidden)
		return
	}
	v, result, e := s.svc.ExecuteSkillTool(r.Context(), humanActor, req.RunID, skill.ToolRequest{Name: "renders.create", ExpectedVersion: req.Version, IdempotencyKey: req.Key})
	skillResponse(w, map[string]any{"run": v, "result": result}, e)
}
