package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
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
type executionScopeArgs struct {
	TaskID            string `json:"task_id" jsonschema:"task identifier"`
	RuntimeInstanceID string `json:"runtime_instance_id,omitempty" jsonschema:"worker process instance from claim"`
	ExecutionID       string `json:"execution_id,omitempty" jsonschema:"execution id from claim"`
	Generation        int    `json:"generation,omitempty" jsonschema:"execution generation from claim"`
	InputSHA256       string `json:"input_sha256,omitempty" jsonschema:"fixed input fingerprint when the call writes a product"`
}
type claimArgs struct {
	RuntimeInstanceID string `json:"runtime_instance_id,omitempty" jsonschema:"new id for this worker process; required for ingest"`
	RequestID         string `json:"request_id,omitempty" jsonschema:"idempotent claim request id; required for ingest"`
}
type progressArgs struct {
	TaskID            string  `json:"task_id" jsonschema:"task identifier"`
	Progress          float64 `json:"progress" jsonschema:"completion ratio in [0,1]; values outside are clamped"`
	Message           string  `json:"message,omitempty" jsonschema:"optional short status message"`
	RuntimeInstanceID string  `json:"runtime_instance_id,omitempty" jsonschema:"worker process instance; required when the task has an ingest execution"`
	ExecutionID       string  `json:"execution_id,omitempty" jsonschema:"execution id from claim; required for ingest"`
	Generation        int     `json:"generation,omitempty" jsonschema:"execution generation from claim; required for ingest"`
}
type heartbeatArgs struct {
	Capability        *preparation.Capability   `json:"capability,omitempty" jsonschema:"optional local capability observation for a fixed profile"`
	IngestCapability  *service.IngestCapability `json:"ingest_capability,omitempty" jsonschema:"optional ingester tool and root-mapping observation, including execution_protocol"`
	TaskID            string                    `json:"task_id,omitempty" jsonschema:"held task to renew; empty only registers liveness"`
	RuntimeInstanceID string                    `json:"runtime_instance_id,omitempty" jsonschema:"worker process instance; required when task_id is an ingest execution"`
	ExecutionID       string                    `json:"execution_id,omitempty" jsonschema:"execution id from claim; required for ingest"`
	Generation        int                       `json:"generation,omitempty" jsonschema:"execution generation from claim; required for ingest"`
}
type checkpointArgs struct {
	TaskID            string `json:"task_id" jsonschema:"held ingest task"`
	ExecutionID       string `json:"execution_id" jsonschema:"execution id from claim"`
	RuntimeInstanceID string `json:"runtime_instance_id" jsonschema:"worker process instance from claim"`
	Generation        int    `json:"generation" jsonschema:"execution generation from claim"`
	Sequence          int    `json:"sequence" jsonschema:"next dense checkpoint sequence starting at 1"`
	InputSHA256       string `json:"input_sha256" jsonschema:"fixed input fingerprint from get_task_input"`
	Kind              string `json:"kind" jsonschema:"copy_chunk, scan_chunk or segment_media"`
	ItemIndex         int    `json:"item_index" jsonschema:"completed block or segment index"`
	Ref               string `json:"ref" jsonschema:"checkpoint manifest path relative to the delivery root"`
	SHA256            string `json:"sha256" jsonschema:"SHA-256 of the checkpoint manifest"`
	ManifestVersion   int    `json:"manifest_version,omitempty" jsonschema:"local manifest version"`
	JournalVersion    int    `json:"journal_version,omitempty" jsonschema:"local journal version"`
	SourceExecutionID string `json:"source_execution_id,omitempty" jsonschema:"earlier execution this checkpoint was recovered from"`
	Summary           string `json:"summary,omitempty" jsonschema:"bounded checkpoint summary"`
}
type ingestResultArgs struct {
	TaskID            string `json:"task_id" jsonschema:"held ingest task"`
	ExecutionID       string `json:"execution_id" jsonschema:"execution id from claim"`
	RuntimeInstanceID string `json:"runtime_instance_id" jsonschema:"worker process instance from claim"`
	Generation        int    `json:"generation" jsonschema:"execution generation from claim"`
	PackageDir        string `json:"package_dir" jsonschema:"published package directory relative to the delivery root"`
	InputSHA256       string `json:"input_sha256" jsonschema:"fixed input fingerprint from get_task_input"`
	RequestID         string `json:"request_id,omitempty" jsonschema:"idempotent submit request id"`
}
type checkpointResult struct {
	OK       bool `json:"ok"`
	Sequence int  `json:"sequence"`
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
	TaskID            string `json:"task_id" jsonschema:"task identifier"`
	Reason            string `json:"reason" jsonschema:"why the task failed"`
	RuntimeInstanceID string `json:"runtime_instance_id,omitempty" jsonschema:"worker process instance; required for ingest"`
	ExecutionID       string `json:"execution_id,omitempty" jsonschema:"execution id from claim; required for ingest"`
	Generation        int    `json:"generation,omitempty" jsonschema:"execution generation from claim; required for ingest"`
}

// claimResult separates an empty queue (claimed=false) from real errors so an
// agent can poll without parsing error text.
type claimResult struct {
	Claimed           bool                    `json:"claimed"`
	Obsolete          bool                    `json:"obsolete,omitempty"`
	Task              *model.Task             `json:"task,omitempty"`
	Scope             *service.ExecutionScope `json:"scope,omitempty"`
	ControlVersion    int                     `json:"control_version,omitempty"`
	LeaseExpiresAt    *time.Time              `json:"lease_expires_at,omitempty"`
	LeaseRemainingMs  int64                   `json:"lease_remaining_ms,omitempty"`
	ExecutionProtocol int                     `json:"execution_protocol,omitempty"`
}
type controlResult struct {
	ExecutionID    string `json:"execution_id"`
	Generation     int    `json:"generation"`
	ControlVersion int    `json:"control_version"`
	Command        string `json:"command"`
	Reason         string `json:"reason,omitempty"`
	Status         string `json:"status"`
	DrainRequired  bool   `json:"drain_required"`
}
type resultStatusResult struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}
type recoveryResult struct {
	Candidates []service.RecoveryCandidate `json:"candidates"`
}
type ackResult struct {
	OK               bool       `json:"ok"`
	LeaseExpiresAt   *time.Time `json:"lease_expires_at,omitempty"`
	LeaseRemainingMs int64      `json:"lease_remaining_ms,omitempty"`
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
	case errors.Is(err, model.ErrStaleExecution):
		return errors.New("stale_execution: this execution is no longer authorized")
	case errors.Is(err, model.ErrResourceBusy):
		return errors.New("resource_busy: an execution still holds the resource")
	case errors.Is(err, model.ErrObsolete):
		return errors.New("obsolete: the claim request no longer matches an active execution")
	case errors.Is(err, model.ErrInstanceConflict):
		return errors.New("instance_conflict: another runtime instance is active for this agent")
	case errors.Is(err, model.ErrProtocolUpgrade):
		return errors.New("execution_protocol_required: upgrade the ingester execution protocol")
	case errors.Is(err, model.ErrConflict):
		return errors.New("conflict: " + lastSegment(err))
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

func scopeFrom(a Agent, args executionScopeArgs) service.ExecutionScope {
	return service.ExecutionScope{
		AgentID: a.ID, Role: a.Role, RuntimeInstanceID: args.RuntimeInstanceID,
		TaskID: args.TaskID, ExecutionID: args.ExecutionID, Generation: args.Generation, InputSHA256: args.InputSHA256,
	}
}

func fillClaim(out service.ClaimOutcome) claimResult {
	res := claimResult{
		Claimed: out.Claimed, Obsolete: out.Obsolete, ControlVersion: out.ControlVersion,
		LeaseExpiresAt: out.LeaseExpiresAt, ExecutionProtocol: out.ExecutionProtocol,
	}
	if out.LeaseRemaining > 0 {
		res.LeaseRemainingMs = out.LeaseRemaining.Milliseconds()
	}
	if out.Scope.ExecutionID != "" {
		scope := out.Scope
		res.Scope = &scope
	}
	if out.Claimed && out.Task.TaskID != "" {
		tk := out.Task
		res.Task = &tk
	}
	return res
}

func requireTaskID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("invalid_argument: task_id is required")
	}
	return nil
}

func registerTaskTools(server *mcp.Server, svc *service.Service, lease time.Duration) {
	mcp.AddTool(server, &mcp.Tool{Name: "claim_task", Description: "Claim the oldest queued task for this agent's role, or get back the task already held. Ingest claims require runtime_instance_id and request_id and return an execution scope."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args claimArgs) (*mcp.CallToolResult, claimResult, error) {
			a := identity(ctx)
			out, err := svc.ClaimForExecution(ctx, a.ID, a.Role, time.Now().UTC().Add(lease), service.ClaimOptions{
				RuntimeInstanceID: args.RuntimeInstanceID, RequestID: args.RequestID,
			})
			if errors.Is(err, model.ErrNotFound) {
				return nil, claimResult{Claimed: false}, nil
			}
			if err != nil {
				return nil, claimResult{}, toolError(err)
			}
			return nil, fillClaim(out), nil
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
			if err := svc.ReportProgressScoped(ctx, service.ExecutionScope{
				AgentID: a.ID, Role: a.Role, RuntimeInstanceID: args.RuntimeInstanceID,
				TaskID: args.TaskID, ExecutionID: args.ExecutionID, Generation: args.Generation,
			}, args.Progress, args.Message); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "heartbeat", Description: "Register liveness and renew the lease of a held task"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args heartbeatArgs) (*mcp.CallToolResult, ackResult, error) {
			a := identity(ctx)
			until := time.Now().UTC().Add(lease)
			if err := svc.HeartbeatScoped(ctx, a.ID, a.Role, args.TaskID, until, service.ExecutionScope{
				RuntimeInstanceID: args.RuntimeInstanceID, ExecutionID: args.ExecutionID, Generation: args.Generation,
			}); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			if args.Capability != nil {
				if err := svc.ReportWorkerCapability(ctx, a.ID, a.Role, *args.Capability); err != nil {
					return nil, ackResult{}, toolError(err)
				}
			}
			if args.IngestCapability != nil {
				if err := svc.ReportIngestCapability(ctx, a.ID, a.Role, *args.IngestCapability); err != nil {
					return nil, ackResult{}, toolError(err)
				}
			}
			if args.TaskID == "" {
				return nil, ackResult{OK: true}, nil
			}
			tk, err := svc.GetTask(ctx, args.TaskID)
			if err != nil {
				return nil, ackResult{}, toolError(err)
			}
			remaining := int64(0)
			if tk.LeaseUntil != nil {
				remaining = max(0, time.Until(*tk.LeaseUntil).Milliseconds())
			}
			return nil, ackResult{OK: true, LeaseExpiresAt: tk.LeaseUntil, LeaseRemainingMs: remaining}, nil
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

	mcp.AddTool(server, &mcp.Tool{Name: "get_task_input", Description: "Read the fixed input package for a task this agent currently holds. Ingest tasks require the claim scope and never return a newer execution."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args executionScopeArgs) (*mcp.CallToolResult, service.TaskInput, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, service.TaskInput{}, err
			}
			in, err := svc.GetTaskInputScoped(ctx, scopeFrom(identity(ctx), args))
			if err != nil {
				return nil, service.TaskInput{}, toolError(err)
			}
			return nil, in, nil
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

	mcp.AddTool(server, &mcp.Tool{Name: "save_ingest_checkpoint", Description: "Register a verified checkpoint manifest for the current execution of a held ingest task"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args checkpointArgs) (*mcp.CallToolResult, checkpointResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, checkpointResult{}, err
			}
			a := identity(ctx)
			seq, err := svc.SaveIngestCheckpoint(ctx, a.ID, a.Role, service.CheckpointRequest(args))
			if err != nil {
				return nil, checkpointResult{}, toolError(err)
			}
			return nil, checkpointResult{OK: true, Sequence: seq}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "submit_ingest_result", Description: "Register the published package of the current execution of a held ingest task"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args ingestResultArgs) (*mcp.CallToolResult, submitResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, submitResult{}, err
			}
			if strings.TrimSpace(args.PackageDir) == "" || strings.TrimSpace(args.ExecutionID) == "" {
				return nil, submitResult{}, errors.New("invalid_argument: package_dir and execution_id are required")
			}
			a := identity(ctx)
			if err := svc.SubmitIngestResult(ctx, a.ID, a.Role, service.IngestResultRequest(args)); err != nil {
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
			if err := svc.FailTaskScoped(ctx, service.ExecutionScope{
				AgentID: a.ID, Role: a.Role, RuntimeInstanceID: args.RuntimeInstanceID,
				TaskID: args.TaskID, ExecutionID: args.ExecutionID, Generation: args.Generation,
			}, args.Reason); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "begin_execution", Description: "Mark the current ingest execution runnable after the previous owner has drained and the server resource barrier is clear"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args executionScopeArgs) (*mcp.CallToolResult, ackResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, ackResult{}, err
			}
			if err := svc.BeginExecution(ctx, scopeFrom(identity(ctx), args)); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_execution_control", Description: "Poll the persisted stop command for this execution. At most one poll waits per execution, for at most 2 seconds. This is not the human SSE stream."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
			executionScopeArgs
			KnownControlVersion int `json:"known_control_version,omitempty" jsonschema:"last control_version this worker observed"`
		}) (*mcp.CallToolResult, controlResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, controlResult{}, err
			}
			view, err := svc.GetExecutionControl(ctx, scopeFrom(identity(ctx), args.executionScopeArgs), args.KnownControlVersion)
			if err != nil {
				return nil, controlResult{}, toolError(err)
			}
			return nil, controlResult{
				ExecutionID: view.ExecutionID, Generation: view.Generation, ControlVersion: view.ControlVersion,
				Command: view.Command, Reason: view.Reason, Status: view.Status, DrainRequired: view.DrainRequired,
			}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ack_execution_stopped", Description: "Confirm that this revoked execution stopped, or that its cleanup is blocked. Does not accept a pid and does not modify the replacement task."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
			executionScopeArgs
			Outcome string `json:"outcome" jsonschema:"stopped or cleanup_blocked"`
		}) (*mcp.CallToolResult, ackResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, ackResult{}, err
			}
			if err := svc.AckExecutionStopped(ctx, scopeFrom(identity(ctx), args.executionScopeArgs), args.Outcome); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_execution_result_status", Description: "Read whether this execution's own submit request is committed, not_committed, obsolete, or conflict"},
		func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
			executionScopeArgs
			RequestID string `json:"request_id" jsonschema:"submit request id previously sent by this instance"`
		}) (*mcp.CallToolResult, resultStatusResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, resultStatusResult{}, err
			}
			st, err := svc.GetExecutionResultStatus(ctx, scopeFrom(identity(ctx), args.executionScopeArgs), args.RequestID)
			if err != nil {
				return nil, resultStatusResult{}, toolError(err)
			}
			return nil, resultStatusResult{RequestID: st.RequestID, Status: st.Status}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "reconcile_execution_drain", Description: "Reconcile a revoked former execution after the local runtime and kernel-owned tree are verified dead. Current claim scope required; no arbitrary pid or path."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
			executionScopeArgs
			FormerExecutionID string `json:"former_execution_id"`
		}) (*mcp.CallToolResult, ackResult, error) {
			if err := svc.ReconcileExecutionDrain(ctx, scopeFrom(identity(ctx), args.executionScopeArgs), args.FormerExecutionID); err != nil {
				return nil, ackResult{}, toolError(err)
			}
			return nil, ackResult{OK: true}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_recovery_candidates", Description: "List bounded earlier executions of the same run and fixed input for the current owner. No absolute paths."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args executionScopeArgs) (*mcp.CallToolResult, recoveryResult, error) {
			if err := requireTaskID(args.TaskID); err != nil {
				return nil, recoveryResult{}, err
			}
			cands, err := svc.ListRecoveryCandidates(ctx, scopeFrom(identity(ctx), args))
			if err != nil {
				return nil, recoveryResult{}, toolError(err)
			}
			return nil, recoveryResult{Candidates: cands}, nil
		})
}
