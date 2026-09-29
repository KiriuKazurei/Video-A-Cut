package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Task loop tools. Every rule (ownership, role, lease, state machine) lives in
// service; these adapters bind the token identity, fix the lease window from
// server configuration, and translate service errors into stable, non-leaky
// messages an agent can branch on.

type taskIDArgs struct {
	TaskID string `json:"task_id" jsonschema:"task identifier"`
}
type progressArgs struct {
	TaskID   string  `json:"task_id" jsonschema:"task identifier"`
	Progress float64 `json:"progress" jsonschema:"completion ratio in [0,1]; values outside are clamped"`
	Message  string  `json:"message,omitempty" jsonschema:"optional short status message"`
}
type heartbeatArgs struct {
	TaskID string `json:"task_id,omitempty" jsonschema:"held task to renew; empty only registers liveness"`
}
type deliveryArgs struct {
	TaskID     string `json:"task_id" jsonschema:"export task identifier"`
	PackageDir string `json:"package_dir" jsonschema:"package directory relative to the control plane delivery root"`
}
type edlResult struct {
	AssetID string         `json:"asset_id"`
	EDL     map[string]any `json:"edl"`
}
type failArgs struct {
	TaskID string `json:"task_id" jsonschema:"task identifier"`
	Reason string `json:"reason" jsonschema:"why the task failed"`
}

// claimResult separates an empty queue (claimed=false) from real errors so an
// agent can poll without parsing error text.
type claimResult struct {
	Claimed bool        `json:"claimed"`
	Task    *model.Task `json:"task,omitempty"`
}
type ackResult struct {
	OK             bool       `json:"ok"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
}
type taskResult struct {
	Task model.Task `json:"task"`
}

const maxMessageLen = 2000

func identity(ctx context.Context) Agent {
	return ctx.Value(identityKey{}).(Agent)
}

// toolError maps service sentinels to fixed codes. Internal wrap chains carry
// task ids and roles of other agents; they are not returned to the caller.
func toolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrNotFound), errors.Is(err, model.ErrForbidden):
		// Forbidden is reported as not_found so a caller cannot probe which
		// task ids exist but belong to someone else.
		return errors.New("not_found: resource does not exist or is not available to this agent")
	case errors.Is(err, model.ErrLeaseExpired):
		return errors.New("lease_expired: lease lapsed; the task may have been requeued")
	case errors.Is(err, model.ErrLeaseHeld):
		return errors.New("lease_held: this agent already holds another live task")
	case errors.Is(err, model.ErrInvalidState):
		return errors.New("invalid_state: task is not in an active state")
	case errors.Is(err, model.ErrArgument):
		return errors.New("invalid_argument: " + lastSegment(err))
	default:
		return errors.New("internal: request failed")
	}
}

func lastSegment(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		// Drop the trailing sentinel text; keep the specific reason before it.
		head := msg[:i]
		if j := strings.LastIndex(head, ": "); j >= 0 {
			return head[j+2:]
		}
		return head
	}
	return msg
}

func requireTaskID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("invalid_argument: task_id is required")
	}
	return nil
}

func registerTaskTools(server *mcp.Server, svc *service.Service, lease time.Duration) {
	mcp.AddTool(server, &mcp.Tool{Name: "claim_task", Description: "Claim the oldest queued task for this agent's role, or get back the task already held"},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, claimResult, error) {
			a := identity(ctx)
			tk, err := svc.ClaimTask(ctx, a.ID, a.Role, time.Now().UTC().Add(lease))
			if errors.Is(err, model.ErrNotFound) {
				return nil, claimResult{Claimed: false}, nil
			}
			if err != nil {
				return nil, claimResult{}, toolError(err)
			}
			return nil, claimResult{Claimed: true, Task: &tk}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "report_progress", Description: "Report progress on a task held by this agent"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args progressArgs) (*mcp.CallToolResult, ackResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, ackResult{}, err
			}
			if len(args.Message) > maxMessageLen {
				return nil, ackResult{}, fmt.Errorf("invalid_argument: message exceeds %d bytes", maxMessageLen)
			}
			a := identity(ctx)
			if err := svc.ReportProgress(ctx, a.ID, args.TaskID, args.Progress, args.Message); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "heartbeat", Description: "Register liveness and renew the lease of a held task"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args heartbeatArgs) (*mcp.CallToolResult, ackResult, error) {
			a := identity(ctx)
			until := time.Now().UTC().Add(lease)
			if err := svc.Heartbeat(ctx, a.ID, a.Role, args.TaskID, until); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			if args.TaskID == "" {
				return nil, ackResult{OK: true}, nil
			}
			tk, err := svc.GetTask(ctx, args.TaskID)
			if err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true, LeaseExpiresAt: tk.LeaseUntil}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_task_status", Description: "Read a task held (or last held) by this agent"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args taskIDArgs) (*mcp.CallToolResult, taskResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, taskResult{}, err
			}
			a := identity(ctx)
			tk, err := svc.GetTask(ctx, args.TaskID)
			if err != nil {
				return nil, taskResult{}, toolError(err)
			}
			// Ownership is checked here because GetTask is an unfiltered read;
			// a requeued task no longer names this agent and becomes invisible.
			if tk.AgentID != a.ID || tk.AgentRole != a.Role {
				return nil, taskResult{}, toolError(model.ErrForbidden)
			}
			return nil, taskResult{Task: tk}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_asset_edl", Description: "Read edl.json of an asset visible to this agent role"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args assetArgs) (*mcp.CallToolResult, edlResult, error) {
			if strings.TrimSpace(args.AssetID) == "" {
				return nil, edlResult{}, errors.New("invalid_argument: asset_id is required")
			}
			raw, err := svc.ReadAssetEDLForRole(ctx, identity(ctx).Role, args.AssetID)
			if err != nil {
				return nil, edlResult{}, toolError(err)
			}
			var edl map[string]any
			if err := json.Unmarshal(raw, &edl); err != nil || edl == nil {
				return nil, edlResult{}, errors.New("invalid_state: stored edl is not a JSON object")
			}
			return nil, edlResult{AssetID: args.AssetID, EDL: edl}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "submit_delivery", Description: "Close a held pipeline task by registering the package (edl.json + delivery-manifest.json) written under the delivery root"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args deliveryArgs) (*mcp.CallToolResult, submitResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, submitResult{}, err
			}
			if strings.TrimSpace(args.PackageDir) == "" {
				return nil, submitResult{}, errors.New("invalid_argument: package_dir is required")
			}
			a := identity(ctx)
			if err := svc.SubmitDelivery(ctx, a.ID, args.TaskID, args.PackageDir); err != nil {
				return nil, submitResult{}, toolError(err)
			}
			return nil, submitResult{Submitted: true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "fail_task", Description: "Close a task held by this agent as failed"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args failArgs) (*mcp.CallToolResult, ackResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, ackResult{}, err
			}
			if strings.TrimSpace(args.Reason) == "" || len(args.Reason) > maxMessageLen {
				return nil, ackResult{}, fmt.Errorf("invalid_argument: reason must be 1..%d bytes", maxMessageLen)
			}
			a := identity(ctx)
			if err := svc.FailTask(ctx, a.ID, args.TaskID, args.Reason); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true}, nil
		})
}
