package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// Execution protocol shared with ingester workers. Older capabilities are not
// given tasks that require this protocol. Control polls wait at most two
// seconds; workers must cap the network timeout at three seconds. SSE is only
// a UI hint and is not an authenticated worker channel.
const (
	ExecutionProtocolVersion = 2
	ControlPollMaxWait       = 2 * time.Second
	ControlNetworkTimeoutMax = 3 * time.Second
	opClaim                  = "claim_task"
	opGetInput               = "get_task_input"
	opControl                = "get_execution_control"
	opHeartbeat              = "heartbeat"
	opProgress               = "report_progress"
	opFail                   = "fail_task"
	opBegin                  = "begin_execution"
	opCheckpoint             = "save_ingest_checkpoint"
	opSubmit                 = "submit_ingest_result"
	opAck                    = "ack_execution_stopped"
	opResult                 = "get_execution_result_status"
	opRecovery               = "list_recovery_candidates"
)

// ExecutionScope is the server-authenticated identity of one ingest attempt.
// Agent and role are filled by the service from the authenticated caller.
// A missing field is not treated as a trusted internal call.
type ExecutionScope struct {
	AgentID           string `json:"agent_id"`
	Role              string `json:"role"`
	RuntimeInstanceID string `json:"runtime_instance_id"`
	TaskID            string `json:"task_id"`
	ExecutionID       string `json:"execution_id"`
	Generation        int    `json:"generation"`
	InputSHA256       string `json:"input_sha256,omitempty"`
}

type executionAuth struct {
	Historical bool
	Current    bool
	Revoked    bool
	Task       model.Task
	Binding    model.IngestTaskBinding
	Execution  model.IngestExecution
	Run        model.IngestRun
}

type controlSlot struct {
	waiting int
	notify  chan struct{}
}

// ClaimOptions carries the optional instance identity on claim_task.
// Ingest claims require both fields. Ordinary tasks ignore them.
type ClaimOptions struct {
	RuntimeInstanceID string
	RequestID         string
}

// ClaimOutcome is the idempotent result of one claim request.
type ClaimOutcome struct {
	Claimed           bool           `json:"claimed"`
	Obsolete          bool           `json:"obsolete,omitempty"`
	Task              model.Task     `json:"-"`
	Scope             ExecutionScope `json:"scope,omitempty"`
	ControlVersion    int            `json:"control_version,omitempty"`
	LeaseExpiresAt    *time.Time     `json:"lease_expires_at,omitempty"`
	LeaseRemaining    time.Duration  `json:"-"`
	ExecutionProtocol int            `json:"execution_protocol,omitempty"`
}

// WithExecutionTx is the single guard around an ingest execution write or
// restricted read. The callback runs only after the scope is authorized, and
// it runs inside the same transaction. File hashing and process waits stay
// outside this transaction.
func (s *Service) WithExecutionTx(ctx context.Context, scope ExecutionScope, operation string, fn func(context.Context, *store.Store, executionAuth) error) error {
	return s.st.Transaction(ctx, func(tx *store.Store) error {
		auth, err := authorizeExecution(ctx, tx, scope, operation)
		if err != nil {
			return err
		}
		return fn(ctx, tx, auth)
	})
}

func scopeComplete(scope ExecutionScope, needInput bool) error {
	if scope.AgentID == "" || scope.Role == "" || scope.RuntimeInstanceID == "" || scope.TaskID == "" || scope.ExecutionID == "" || scope.Generation < 1 {
		return fmt.Errorf("execution scope is incomplete: %w", model.ErrArgument)
	}
	if !idemKeyPattern.MatchString(scope.RuntimeInstanceID) || !idemKeyPattern.MatchString(scope.ExecutionID) {
		return fmt.Errorf("execution scope identity is invalid: %w", model.ErrArgument)
	}
	if needInput && (len(scope.InputSHA256) != 64 || !isHex(scope.InputSHA256)) {
		return fmt.Errorf("input_sha256 is required: %w", model.ErrArgument)
	}
	return nil
}

func staleExecution(msg string) error {
	return fmt.Errorf("%s: %w", msg, errors.Join(model.ErrStaleExecution, model.ErrConflict))
}

func executionCurrent(status string) bool {
	switch status {
	case model.ExecAllocated, model.ExecWaitingResource, model.ExecRunning, model.ExecSuspended:
		return true
	default:
		return false
	}
}

func executionRevoked(status string) bool {
	switch status {
	case model.ExecStopRequested, model.ExecDraining, model.ExecStopped, model.ExecCleanupBlocked:
		return true
	default:
		return false
	}
}

func executionTerminal(status string) bool {
	switch status {
	case model.ExecStopped, model.ExecSucceeded, model.ExecFailed:
		return true
	default:
		return false
	}
}

func scopeFromExecution(e model.IngestExecution) ExecutionScope {
	return ExecutionScope{
		AgentID: e.AgentID, Role: e.Role, RuntimeInstanceID: e.RuntimeInstanceID,
		TaskID: e.TaskID, ExecutionID: e.ExecutionID, Generation: e.Generation, InputSHA256: e.InputSHA256,
	}
}

func sourceSnapshotKey(sourceID string) string {
	if sourceID == "" || strings.Contains(sourceID, "/") || strings.Contains(sourceID, "\\") || strings.Contains(sourceID, "..") || strings.ContainsAny(sourceID, " \t") {
		return ""
	}
	return "source_snapshot:" + sourceID
}

// authorizeExecution enforces one operation against the stored execution.
// Historical tasks without an ingest binding pass through. A bound task with
// a missing, old, or mismatched scope is rejected. A revoked execution's
// original instance may still read its own stop state, acknowledge it, and
// query its own submit.
func authorizeExecution(ctx context.Context, tx *store.Store, scope ExecutionScope, operation string) (executionAuth, error) {
	var auth executionAuth
	if scope.TaskID == "" {
		if operation == opHeartbeat {
			auth.Historical = true
			return auth, nil
		}
		return auth, fmt.Errorf("task_id is required: %w", model.ErrArgument)
	}
	tk, err := tx.GetTask(ctx, scope.TaskID)
	if err != nil {
		return auth, err
	}
	auth.Task = tk
	b, err := tx.GetIngestBinding(ctx, scope.TaskID)
	if errors.Is(err, model.ErrNotFound) {
		auth.Historical = true
		return auth, nil
	}
	if err != nil {
		return auth, err
	}
	auth.Binding = b
	needInput := operation == opCheckpoint || operation == opSubmit
	if err := scopeComplete(scope, needInput); err != nil {
		return auth, err
	}
	if tk.AgentID != "" && tk.AgentID != scope.AgentID && operation != opControl && operation != opAck && operation != opResult {
		// A requeued task clears the holder. Revoked-holder reads are checked
		// against the execution row, not the task's current holder.
		if executionCurrent(tk.Status) || tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning {
			if tk.AgentID != scope.AgentID {
				return auth, fmt.Errorf("agent is not the execution holder: %w", model.ErrForbidden)
			}
		}
	}
	ex, err := tx.GetIngestExecution(ctx, scope.ExecutionID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return auth, staleExecution("execution_id is unknown")
		}
		return auth, err
	}
	auth.Execution = ex
	if ex.AgentID != scope.AgentID || ex.Role != scope.Role {
		return auth, fmt.Errorf("execution belongs to another agent: %w", model.ErrForbidden)
	}
	if ex.RuntimeInstanceID != scope.RuntimeInstanceID {
		return auth, fmt.Errorf("runtime instance does not hold this execution: %w", model.ErrForbidden)
	}
	if ex.TaskID != scope.TaskID || ex.Generation != scope.Generation {
		return auth, staleExecution("execution generation does not match")
	}
	run, err := tx.GetIngestRun(ctx, ex.RunID)
	if err != nil {
		return auth, err
	}
	auth.Run = run
	current := b.ExecutionID == ex.ExecutionID && b.ExecutionSeq == ex.Generation && executionCurrent(ex.Status)
	revoked := executionRevoked(ex.Status) || (b.ExecutionID != ex.ExecutionID && !executionCurrent(ex.Status))
	if b.ExecutionID != ex.ExecutionID && executionRevoked(ex.Status) {
		revoked = true
		current = false
	}
	if b.ExecutionID != ex.ExecutionID && (ex.Status == model.ExecSucceeded || ex.Status == model.ExecFailed) {
		revoked = false
		current = false
	}
	auth.Current, auth.Revoked = current, revoked
	if needInput && scope.InputSHA256 != b.InputSHA256 {
		return auth, staleExecution("input_sha256 does not match the fixed task input")
	}
	if err := permitOperation(ctx, tx, auth, scope, operation); err != nil {
		return auth, err
	}
	return auth, nil
}

func permitOperation(ctx context.Context, tx *store.Store, auth executionAuth, scope ExecutionScope, operation string) error {
	switch operation {
	case opControl, opAck, opResult:
		if auth.Revoked || (auth.Current && executionRevoked(auth.Execution.Status)) || executionRevoked(auth.Execution.Status) {
			return nil
		}
		if operation == opControl && (auth.Current || executionTerminal(auth.Execution.Status)) {
			return nil
		}
		if operation == opResult && (auth.Current || executionTerminal(auth.Execution.Status)) {
			return nil
		}
		return staleExecution("execution is not the caller's revoked or current execution")
	case opRecovery, opGetInput:
		if !auth.Current || auth.Execution.Status == model.ExecSuspended {
			return staleExecution("execution cannot read the current task input")
		}
		return governanceAllows(ctx, tx, auth, scope.Role)
	case opBegin:
		if !auth.Current {
			return staleExecution("begin_execution requires the current execution")
		}
		if auth.Execution.Status != model.ExecAllocated && auth.Execution.Status != model.ExecWaitingResource && auth.Execution.Status != model.ExecRunning {
			return fmt.Errorf("execution is %s: %w", auth.Execution.Status, model.ErrInvalidState)
		}
		if leaseExpired(auth.Task) {
			return model.ErrLeaseExpired
		}
		return governanceAllows(ctx, tx, auth, scope.Role)
	case opHeartbeat:
		if !auth.Current || auth.Execution.Status == model.ExecSuspended {
			return staleExecution("heartbeat cannot renew this execution")
		}
		return nil
	case opProgress, opCheckpoint, opSubmit:
		if !auth.Current {
			return staleExecution("execution_id is stale; the task was claimed again")
		}
		if auth.Execution.Status != model.ExecRunning {
			return fmt.Errorf("begin_execution is required before writing: %w", model.ErrInvalidState)
		}
		if leaseExpired(auth.Task) {
			return model.ErrLeaseExpired
		}
		if auth.Binding.Invalidated || auth.Run.State == "cancelled" || auth.Run.CurrentTaskID != auth.Task.TaskID {
			return fmt.Errorf("ingest task was cancelled or superseded: %w", model.ErrConflict)
		}
		return governanceAllows(ctx, tx, auth, scope.Role)
	case opFail:
		if !auth.Current || !executionCurrent(auth.Execution.Status) || auth.Execution.Status == model.ExecSuspended {
			return staleExecution("execution_id is stale; the task was claimed again")
		}
		if leaseExpired(auth.Task) {
			return model.ErrLeaseExpired
		}
		return nil
	default:
		return fmt.Errorf("unknown execution operation: %w", model.ErrArgument)
	}
}

func governanceAllows(ctx context.Context, tx *store.Store, auth executionAuth, role string) error {
	asset, err := tx.GetAsset(ctx, auth.Task.AssetID)
	if err != nil {
		return err
	}
	if !assetVisibleForRole(asset, role) {
		return fmt.Errorf("asset governance no longer allows the ingester: %w", model.ErrForbidden)
	}
	return nil
}

func (s *Service) controlPollWait() time.Duration {
	s.ctrlMu.Lock()
	defer s.ctrlMu.Unlock()
	if !s.controlWaitSet {
		return ControlPollMaxWait
	}
	if s.controlWait < 0 {
		return 0
	}
	if s.controlWait > ControlPollMaxWait {
		return ControlPollMaxWait
	}
	return s.controlWait
}

// SetControlPollWait caps how long one control poll blocks. Values above the
// protocol maximum are clamped. A negative duration returns immediately.
func (s *Service) SetControlPollWait(d time.Duration) {
	s.ctrlMu.Lock()
	s.controlWait = d
	s.controlWaitSet = true
	s.ctrlMu.Unlock()
}

// SetControlPollHook runs once a poll is waiting and is for tests that need
// to publish a control change without sleeping.
func (s *Service) SetControlPollHook(fn func()) {
	s.ctrlMu.Lock()
	s.controlHook = fn
	s.ctrlMu.Unlock()
}

func (s *Service) finishControl(ids []string) {
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		s.wakeControl(id)
		s.publish("execution.changed", map[string]any{"execution_id": id})
	}
}

func (s *Service) wakeControl(executionID string) {
	s.ctrlMu.Lock()
	defer s.ctrlMu.Unlock()
	sl := s.ctrl[executionID]
	if sl == nil {
		return
	}
	select {
	case sl.notify <- struct{}{}:
	default:
	}
}

func (s *Service) beginControlWait(executionID string) (*controlSlot, error) {
	s.ctrlMu.Lock()
	defer s.ctrlMu.Unlock()
	if s.ctrl == nil {
		s.ctrl = map[string]*controlSlot{}
	}
	sl := s.ctrl[executionID]
	if sl == nil {
		sl = &controlSlot{notify: make(chan struct{}, 1)}
		s.ctrl[executionID] = sl
	}
	if sl.waiting >= 1 {
		return nil, fmt.Errorf("this execution already has a control poll: %w", model.ErrConflict)
	}
	sl.waiting++
	return sl, nil
}

func (s *Service) endControlWait(executionID string) {
	s.ctrlMu.Lock()
	defer s.ctrlMu.Unlock()
	if sl := s.ctrl[executionID]; sl != nil && sl.waiting > 0 {
		sl.waiting--
	}
}
