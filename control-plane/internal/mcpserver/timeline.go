package mcpserver

import (
	"context"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addProjectTimeline(server *mcp.Server, svc *service.Service) {
	mcp.AddTool(server, &mcp.Tool{Name: "sequences.list", Description: "List sequences belonging to the single currently open AVP. Video/audio stream indices and integer frame positions are authoritative; edit through an allowed Skill run timeline.edit tool."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		ProjectID string `json:"project_id"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		if a.ProjectID != svc.CurrentProjectID() {
			return nil, nil, model.ErrNotFound
		}
		view, e := svc.ProjectWorkspace(ctx)
		if e != nil {
			return nil, nil, e
		}
		if view.Document == nil {
			return nil, nil, model.ErrNotFound
		}
		return nil, map[string]any{"session": view.ID, "sequences": view.Document.Sequences}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "sequences.get", Description: "Read a current-project sequence, its revision and content version. Human edit history is private. Source and sequence markers are distinct; single points are valid one-frame anchors."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		ProjectID  string `json:"project_id"`
		SequenceID string `json:"sequence_id"`
	}) (*mcp.CallToolResult, service.TimelineView, error) {
		v, e := svc.Timeline(ctx, a.ProjectID, a.SequenceID)
		v.Sequence.History = nil
		v.Sequence.HistoryCursor = 0
		v.History = nil
		return nil, v, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "references.list", Description: "List typed current-project resources and marker occurrences. Duplicate recognition names are allowed: retain target_id, sequence_id and clip_id of the selected candidate. Never choose a first name match."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		ProjectID  string `json:"project_id"`
		SequenceID string `json:"sequence_id,omitempty"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		v, e := svc.ReferenceDirectory(a.ProjectID, a.SequenceID)
		return nil, map[string]any{"references": v}, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "references.resolve", Description: "Resolve explicit typed stable IDs into current source timestamps and sequence frames; a missing/deleted/trimmed anchor returns a conflict, never a same-name substitute."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		ProjectID  string              `json:"project_id"`
		References []project.Reference `json:"references"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		v, e := svc.ResolveProjectReferences(a.ProjectID, a.References)
		return nil, map[string]any{"references": v}, e
	})
}
