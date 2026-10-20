package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type projectResourceArgs struct {
	ProjectID  string `json:"project_id"`
	ResourceID string `json:"resource_id"`
}

func addProjectResources(server *mcp.Server, svc *service.Service) {
	mcp.AddTool(server, &mcp.Tool{Name: "resources.list", Description: "List all imported resources of the currently open AVP project. No visibility confirmation is required. Includes video, audio, images, subtitles, documents and scripts."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		ProjectID string `json:"project_id"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		resources, err := svc.ProjectResources(a.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		out := []project.Resource{}
		for _, r := range resources {
			if r.Removed {
				continue
			}
			r.Source.OriginalPath, r.Source.RelativePath = "", ""
			if format, ok := r.Metadata["format"].(map[string]any); ok {
				delete(format, "filename")
			}
			out = append(out, r)
		}
		return nil, map[string]any{"project_id": a.ProjectID, "resources": out}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "resources.read", Description: "Read a current-project text, subtitle or script resource, or receive an imported image as native image content. For video/audio use the media tools in a Skill run. Never accepts filesystem paths."}, func(ctx context.Context, _ *mcp.CallToolRequest, a projectResourceArgs) (*mcp.CallToolResult, map[string]any, error) {
		r, err := svc.ProjectResource(a.ProjectID, a.ResourceID)
		if err != nil {
			return nil, nil, err
		}
		if r.Removed {
			return nil, nil, model.ErrNotFound
		}
		out := map[string]any{"project_id": a.ProjectID, "resource_id": r.ID, "kind": r.Kind, "name": r.Name}
		if r.Kind == "script" {
			view, err := svc.GetScriptWorkbench(ctx, r.ScriptID)
			if err != nil {
				return nil, nil, err
			}
			out["script"] = view
			return nil, out, nil
		}
		if r.Kind != "document" && r.Kind != "subtitle" && r.Kind != "image" {
			return nil, nil, model.ErrArgument
		}
		path, err := svc.ProjectResourcePath(a.ProjectID, a.ResourceID, false)
		if err != nil {
			return nil, nil, err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
		if err != nil {
			return nil, nil, err
		}
		if len(raw) > 8<<20 {
			return nil, nil, model.ErrArgument
		}
		if r.Kind == "image" {
			mime := http.DetectContentType(raw)
			if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" && mime != "image/gif" {
				proxy, e := svc.CreateResourcePreview(ctx, a.ProjectID, a.ResourceID)
				if e != nil {
					return nil, nil, e
				}
				raw, e = os.ReadFile(proxy)
				if e != nil {
					return nil, nil, e
				}
				if len(raw) > 8<<20 {
					return nil, nil, model.ErrArgument
				}
				mime = "image/png"
			}
			metadata, _ := json.Marshal(out)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(metadata)}, &mcp.ImageContent{Data: raw, MIMEType: mime}}}, out, nil
		}
		out["text"] = string(raw)
		return nil, out, nil
	})
}
