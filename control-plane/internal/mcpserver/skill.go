package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type skillIDArgs struct {
	ID string `json:"id"`
}
type skillResourceArgs struct {
	ID       string `json:"id"`
	Resource string `json:"resource"`
	SHA      string `json:"content_sha256"`
}
type skillRunIDArgs struct {
	RunID string `json:"run_id"`
}
type skillClaimArgs struct {
	RunID   string `json:"run_id"`
	Version int    `json:"expected_version"`
}
type skillToolArgs struct {
	RunID   string `json:"run_id"`
	Request struct {
		Name            string         `json:"name"`
		Arguments       map[string]any `json:"arguments"`
		ExpectedVersion int            `json:"expected_version"`
		IdempotencyKey  string         `json:"idempotency_key"`
	} `json:"request"`
}

func mcpSkillActor(ctx context.Context) string { return "agent:" + ctx.Value(identityKey{}).(Agent).ID }
func requireSkillSessionRole(ctx context.Context, svc *service.Service, id string) error {
	r, err := svc.GetSkillRun(ctx, mcpSkillActor(ctx), id)
	if err != nil {
		return err
	}
	if r.AgentRole != ctx.Value(identityKey{}).(Agent).Role || r.Mode != "external_mcp" {
		return model.ErrForbidden
	}
	return nil
}
func registerSkillTools(server *mcp.Server, svc *service.Service) {
	mcp.AddTool(server, &mcp.Tool{Name: "skill_runs.steps", Description: "Read this Agent run's durable tool results and decision details"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillRunIDArgs) (*mcp.CallToolResult, map[string]any, error) {
		if e := requireSkillSessionRole(ctx, svc, a.RunID); e != nil {
			return nil, nil, e
		}
		v, e := svc.SkillRunSteps(ctx, mcpSkillActor(ctx), a.RunID)
		return nil, map[string]any{"steps": v}, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skills.list", Description: "List VAC Skill packages and their declared tool contracts"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		v, e := svc.ListSkills(ctx)
		return nil, map[string]any{"skills": v}, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skills.get", Description: "Read the complete VAC Skill, version, content hash and reference resource index"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillIDArgs) (*mcp.CallToolResult, skill.Package, error) {
		v, e := svc.GetSkill(ctx, a.ID)
		return nil, v, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skills.read_resource", Description: "Read a text resource from the exact Skill package hash; never an arbitrary disk path"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillResourceArgs) (*mcp.CallToolResult, map[string]string, error) {
		p, e := svc.GetSkill(ctx, a.ID)
		if e != nil {
			return nil, nil, e
		}
		if p.SHA != a.SHA {
			return nil, nil, model.ErrConflict
		}
		text, ok := p.Resources[a.Resource]
		if !ok {
			return nil, nil, model.ErrNotFound
		}
		return nil, map[string]string{"content": text}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "projects.list", Description: "List only projects a human has authorized for external Agent access"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		rows, e := svc.ListProjects(ctx)
		if e != nil {
			return nil, nil, e
		}
		out := []skill.Project{}
		for _, raw := range rows {
			var p skill.Project
			if json.Unmarshal(raw, &p) == nil && p.ExternalAuthorized {
				out = append(out, p)
			}
		}
		return nil, map[string]any{"projects": out}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "projects.visible_set.get", Description: "Read confirmed project-visible media metadata without filesystem paths"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillIDArgs) (*mcp.CallToolResult, skill.VisibleSet, error) {
		p, e := svc.GetProject(ctx, a.ID)
		if e != nil {
			return nil, skill.VisibleSet{}, e
		}
		if !p.ExternalAuthorized {
			return nil, skill.VisibleSet{}, model.ErrForbidden
		}
		v, e := svc.ProjectAgentView(ctx, a.ID, ctx.Value(identityKey{}).(Agent).Role)
		return nil, v, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skill_runs.create", Description: "Create a project-scoped external Agent run; human project authorization is required"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skill.RunRequest) (*mcp.CallToolResult, skill.Run, error) {
		a.AgentRole = ctx.Value(identityKey{}).(Agent).Role
		v, e := svc.CreateSkillRun(ctx, mcpSkillActor(ctx), a)
		return nil, v, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skill_runs.claim", Description: "Claim a human-created external Agent run for this authenticated Agent"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillClaimArgs) (*mcp.CallToolResult, skill.Run, error) {
		v, e := svc.ClaimSkillRun(ctx, mcpSkillActor(ctx), a.RunID, a.Version, ctx.Value(identityKey{}).(Agent).Role)
		return nil, v, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skill_runs.get", Description: "Read this Agent's run and its immutable Skill/media context"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillRunIDArgs) (*mcp.CallToolResult, map[string]any, error) {
		if e := requireSkillSessionRole(ctx, svc, a.RunID); e != nil {
			return nil, nil, e
		}
		v, e := svc.SkillRunContext(ctx, mcpSkillActor(ctx), a.RunID)
		return nil, v, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skill_runs.cancel", Description: "Cancel this Agent's run, stopping active media/model processes"}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillClaimArgs) (*mcp.CallToolResult, skill.Run, error) {
		v, e := svc.UpdateSkillRun(ctx, mcpSkillActor(ctx), a.RunID, "cancel", a.Version)
		return nil, v, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "skill_runs.tool", Description: "Invoke a declared tool with version and idempotency key. media.list/get_frames/transcribe; summaries.get/submit; scripts.propose; renders.create; artifacts.list/get; run.finish. Human finalization is never available."}, func(ctx context.Context, _ *mcp.CallToolRequest, a skillToolArgs) (*mcp.CallToolResult, map[string]any, error) {
		if e := requireSkillSessionRole(ctx, svc, a.RunID); e != nil {
			return nil, nil, e
		}
		args, e := json.Marshal(a.Request.Arguments)
		if e != nil {
			return nil, nil, model.ErrArgument
		}
		r, result, e := svc.ExecuteSkillTool(ctx, mcpSkillActor(ctx), a.RunID, skill.ToolRequest{
			Name: a.Request.Name, Arguments: args, ExpectedVersion: a.Request.ExpectedVersion, IdempotencyKey: a.Request.IdempotencyKey,
		})
		return nil, map[string]any{"run": r, "result": result}, e
	})
}
