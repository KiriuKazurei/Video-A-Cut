package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func (s *Server) projectRoutes() {
	s.timelineRoutes()
	s.drawRoutes()
	s.mux.HandleFunc("POST /api/workspace/inspect-project", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeServiceError(w, err)
			return
		}
		v, e := s.svc.InspectProjectFile(r.Context(), req.Path)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("PATCH /api/projects/{id}/scripts/{script}/bindings", func(w http.ResponseWriter, r *http.Request) {
		var req service.ScriptFileImport
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.BindProjectScript(r.Context(), r.PathValue("id"), r.PathValue("script"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("PATCH /api/workspace/state", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Session string         `json:"session"`
			State   map[string]any `json:"state"`
		}
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.UpdateProjectWorkspaceState(r.Context(), req.Session, req.State)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/workspace", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.ProjectWorkspace(r.Context())
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/workspace/{action}", func(w http.ResponseWriter, r *http.Request) {
		var req service.ProjectCommand
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ProjectFileCommand(r.Context(), r.PathValue("action"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/workspace/providers", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.svc.ProviderPolicy()) })
	s.mux.HandleFunc("PUT /api/workspace/providers", func(w http.ResponseWriter, r *http.Request) {
		var req service.ProviderPolicy
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.SetProviderPolicy(req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/resources", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.svc.ProjectResources(r.PathValue("id"))
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/resources", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ImportProjectResource(r.Context(), r.PathValue("id"), req.Path)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("PATCH /api/projects/{id}/resources/{resource}", func(w http.ResponseWriter, r *http.Request) {
		var req service.ResourcePatch
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.UpdateProjectResource(r.Context(), r.PathValue("id"), r.PathValue("resource"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("GET /api/projects/{id}/resources/{resource}/content", s.projectResourceContent)
	s.mux.HandleFunc("POST /api/projects/{id}/resources/{resource}/preview", func(w http.ResponseWriter, r *http.Request) {
		_, e := s.svc.CreateResourcePreview(r.Context(), r.PathValue("id"), r.PathValue("resource"))
		skillResponse(w, map[string]bool{"ready": e == nil}, e)
	})
	s.mux.HandleFunc("POST /api/workspace/inspect-script", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.InspectScriptFile(req.Path)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/scripts/import-file", func(w http.ResponseWriter, r *http.Request) {
		var req service.ScriptFileImport
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		v, e := s.svc.ImportScriptFile(r.Context(), r.PathValue("id"), req)
		skillResponse(w, v, e)
	})
	s.mux.HandleFunc("POST /api/projects/{id}/scripts/{script}/export-file", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if e := decodeJSON(w, r, &req); e != nil {
			writeServiceError(w, e)
			return
		}
		e := s.svc.ExportScriptFile(r.Context(), r.PathValue("id"), r.PathValue("script"), req.Path)
		skillResponse(w, map[string]bool{"saved": e == nil}, e)
	})
}

func (s *Server) projectResourceContent(w http.ResponseWriter, r *http.Request) {
	resource, err := s.svc.ProjectResource(r.PathValue("id"), r.PathValue("resource"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if resource.Kind == "script" {
		view, err := s.svc.GetScriptWorkbench(r.Context(), resource.ScriptID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(view.Script.Body))
		return
	}
	path, err := s.svc.ProjectResourcePath(r.PathValue("id"), r.PathValue("resource"), r.URL.Query().Get("proxy") == "1")
	if resource.Draw != nil {
		path, err = s.svc.CreateResourcePreview(r.Context(), r.PathValue("id"), r.PathValue("resource"))
	}
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if resource.Kind == "document" || resource.Kind == "subtitle" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeFile(w, r, path)
}

func (s *Server) workspaceGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.svc.WorkspaceEnabled() || !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api/workspace") || r.URL.Path == "/api/events" {
			next.ServeHTTP(w, r)
			return
		}
		if (r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/api/assets/")) || (r.Method == "POST" && (r.URL.Path == "/api/recordings" || r.URL.Path == "/api/deliveries/import" || r.URL.Path == "/api/assets/visibility" || (strings.HasPrefix(r.URL.Path, "/api/projects/") && strings.HasSuffix(r.URL.Path, "/media")))) || (r.Method == "POST" && r.URL.Path == "/api/projects") || (r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/projects/")) || strings.HasSuffix(r.URL.Path, "/visible-set/confirm") {
			writeError(w, http.StatusGone, "retired_project_flow", "请使用工程文件和资源接口")
			return
		}
		ctx, release, err := s.svc.BeginProjectRequest(r.Context(), r.Header.Get("X-VAC-Project-Session"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		defer release()
		r = r.WithContext(ctx)
		if s.svc.CurrentProjectID() == "" {
			for _, prefix := range []string{"/api/assets", "/api/tasks", "/api/workflows", "/api/scripts", "/api/projects", "/api/skill-runs", "/api/ingest/runs", "/api/visible-set", "/api/tts/previews"} {
				if strings.HasPrefix(r.URL.Path, prefix) {
					if r.Method == "GET" && (r.URL.Path == "/api/assets" || r.URL.Path == "/api/projects" || r.URL.Path == "/api/skill-runs") {
						writeJSON(w, 200, []json.RawMessage{})
						return
					}
					writeServiceError(w, model.ErrNotFound)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
