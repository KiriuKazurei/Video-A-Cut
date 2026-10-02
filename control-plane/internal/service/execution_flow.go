package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func normalizeResourcePolicy(policy string) (string, error) {
	if policy == "" {
		return model.ResourceRejectIfBusy, nil
	}
	if policy == model.ResourceRejectIfBusy || policy == model.ResourceReplaceAfterStop {
		return policy, nil
	}
	return "", fmt.Errorf("resource_policy must be reject_if_busy or replace_after_stop: %w", model.ErrArgument)
}

func rejectAgentPreempt(actor, policy string) error {
	if policy == model.ResourceReplaceAfterStop && strings.HasPrefix(actor, "agent:") {
		return fmt.Errorf("agents cannot force preemption: %w", model.ErrForbidden)
	}
	return nil
}

func resourceOccupied(res model.ExecutionResource) bool {
	if res.Barrier || res.State == model.ResourceBlocked || res.State == model.ResourceDraining {
		return true
	}
	return res.State == model.ResourceHeld && res.OwnerExecutionID != ""
}

func (s *Service) revokeExecution(ctx context.Context, tx *store.Store, ex model.IngestExecution, reason string, wake *[]string) error {
	if ex.ExecutionID == "" || ex.Status == model.ExecSucceeded || ex.Status == model.ExecFailed || ex.Status == model.ExecStopped {
		return nil
	}
	now := time.Now().UTC()
	reason = sanitizeReason(reason)
	original := ex.Status
	if original != model.ExecStopRequested && original != model.ExecDraining && original != model.ExecCleanupBlocked {
		ex.Status = model.ExecStopRequested
		ex.StopReason = reason
		ex.LeaseUntil = nil
		if err := tx.UpdateIngestExecution(ctx, ex); err != nil {
			return err
		}
	}
	if err := tx.RequestStop(ctx, ex.ExecutionID, reason, now); err != nil {
		return err
	}
	run, err := tx.GetIngestRun(ctx, ex.RunID)
	if err != nil {
		return err
	}
	key := sourceSnapshotKey(run.SourceID)
	if key == "" {
		return fmt.Errorf("recording has no server resource key: %w", model.ErrInvalidState)
	}
	res, err := tx.GetExecutionResource(ctx, key)
	state := model.ResourceDraining
	owner, gen := ex.ExecutionID, ex.Generation
	if err == nil {
		if res.OwnerExecutionID != "" {
			owner, gen = res.OwnerExecutionID, res.OwnerGeneration
		}
		if res.State == model.ResourceBlocked || original == model.ExecCleanupBlocked {
			state = model.ResourceBlocked
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return err
	}
	if original == model.ExecCleanupBlocked {
		state = model.ResourceBlocked
	}
	if err := tx.UpsertExecutionResource(ctx, model.ExecutionResource{
		ResourceKey: key, OwnerExecutionID: owner, OwnerGeneration: gen,
		Strategy: model.ResourceReplaceAfterStop, State: state, Barrier: true, UpdatedAt: now,
	}); err != nil {
		return err
	}
	if wake != nil {
		*wake = append(*wake, ex.ExecutionID)
	}
	return nil
}

func (s *Service) releaseResourceIfOwner(ctx context.Context, tx *store.Store, run model.IngestRun, executionID string) error {
	key := sourceSnapshotKey(run.SourceID)
	if key == "" {
		return nil
	}
	res, err := tx.GetExecutionResource(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if res.OwnerExecutionID != executionID || res.Barrier || res.State == model.ResourceBlocked {
		return nil
	}
	res.OwnerExecutionID, res.OwnerGeneration = "", 0
	res.State, res.Barrier = model.ResourceReleased, false
	res.UpdatedAt = time.Now().UTC()
	return tx.UpsertExecutionResource(ctx, res)
}

func liveExecutionOf(ctx context.Context, tx *store.Store, run model.IngestRun) (model.IngestExecution, bool, error) {
	if run.CurrentTaskID == "" {
		return model.IngestExecution{}, false, nil
	}
	b, err := tx.GetIngestBinding(ctx, run.CurrentTaskID)
	if errors.Is(err, model.ErrNotFound) || b.ExecutionID == "" {
		return model.IngestExecution{}, false, nil
	}
	if err != nil {
		return model.IngestExecution{}, false, err
	}
	ex, err := tx.GetIngestExecution(ctx, b.ExecutionID)
	if errors.Is(err, model.ErrNotFound) {
		return model.IngestExecution{}, false, nil
	}
	if err != nil {
		return model.IngestExecution{}, false, err
	}
	return ex, executionCurrent(ex.Status), nil
}

// prepareStartSlot applies reject_if_busy or replace_after_stop before a new
// run is inserted. A busy reject writes nothing. A replace revokes the old
// execution, persists stop, and retires the old run in this same transaction.
func (s *Service) prepareStartSlot(ctx context.Context, tx *store.Store, assetID, actor, policy string, wake *[]string) (bool, error) {
	policy, err := normalizeResourcePolicy(policy)
	if err != nil {
		return false, err
	}
	if err := rejectAgentPreempt(actor, policy); err != nil {
		return false, err
	}
	active, err := tx.ActiveIngestRun(ctx, assetID)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ex, live, err := liveExecutionOf(ctx, tx, active)
	if err != nil {
		return false, err
	}
	if !live {
		return false, fmt.Errorf("asset already has an active ingest run: %w", model.ErrConflict)
	}
	if policy == model.ResourceRejectIfBusy {
		return false, fmt.Errorf("an execution holds this recording: %w", model.ErrResourceBusy)
	}
	if err := s.revokeExecution(ctx, tx, ex, "replace_after_stop", wake); err != nil {
		return false, err
	}
	b, err := tx.GetIngestBinding(ctx, active.CurrentTaskID)
	if err != nil {
		return false, err
	}
	b.Invalidated = true
	b.HandoverState = "draining"
	if err := tx.UpdateIngestBinding(ctx, b); err != nil {
		return false, err
	}
	tk, err := tx.GetTask(ctx, active.CurrentTaskID)
	if err != nil {
		return false, err
	}
	if tk.Status == model.TaskStatusQueued || tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning {
		tk.Status, tk.LeaseUntil, tk.Message = model.TaskStatusCancelled, nil, "replaced by a new ingest run"
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return false, err
		}
	}
	prev := active.Version
	active.State, active.ErrorCode, active.ErrorMessage = ingest.StateCancelled, "replaced", "replace_after_stop"
	active.Version++
	if err := tx.UpdateIngestRun(ctx, active, prev); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) allocateIngestExecution(ctx context.Context, tx *store.Store, agentID, role string, opt ClaimOptions, tk model.Task, lease time.Time, wake *[]string) (model.IngestExecution, error) {
	if !idemKeyPattern.MatchString(opt.RuntimeInstanceID) || !idemKeyPattern.MatchString(opt.RequestID) {
		return model.IngestExecution{}, fmt.Errorf("ingest claim requires runtime_instance_id and request_id: %w", model.ErrArgument)
	}
	b, err := tx.GetIngestBinding(ctx, tk.TaskID)
	if err != nil {
		return model.IngestExecution{}, err
	}
	run, err := tx.GetIngestRun(ctx, b.RunID)
	if err != nil {
		return model.IngestExecution{}, err
	}
	lives, err := tx.ListLiveExecutionsByAgent(ctx, agentID)
	if err != nil {
		return model.IngestExecution{}, err
	}
	for _, live := range lives {
		if live.RuntimeInstanceID == opt.RuntimeInstanceID {
			continue
		}
		lb, err := tx.GetIngestBinding(ctx, live.TaskID)
		if err != nil || lb.ExecutionID != live.ExecutionID {
			continue
		}
		return model.IngestExecution{}, fmt.Errorf("another runtime instance holds a live execution: %w", model.ErrInstanceConflict)
	}
	barrier := false
	if b.ExecutionID != "" {
		prev, err := tx.GetIngestExecution(ctx, b.ExecutionID)
		if err == nil && (executionCurrent(prev.Status) || prev.Status == model.ExecStopRequested || prev.Status == model.ExecDraining || prev.Status == model.ExecCleanupBlocked) {
			if executionCurrent(prev.Status) {
				if err := s.revokeExecution(ctx, tx, prev, "superseded", wake); err != nil {
					return model.IngestExecution{}, err
				}
			}
			barrier = true
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			return model.IngestExecution{}, err
		}
	}
	key := sourceSnapshotKey(run.SourceID)
	res, err := tx.GetExecutionResource(ctx, key)
	if err == nil && resourceOccupied(res) {
		barrier = true
	} else if err != nil && !errors.Is(err, model.ErrNotFound) {
		return model.IngestExecution{}, err
	}
	now := time.Now().UTC()
	ex := model.IngestExecution{
		ExecutionID: newID("exe"), TaskID: tk.TaskID, RunID: b.RunID, Generation: b.ExecutionSeq + 1,
		AgentID: agentID, Role: role, RuntimeInstanceID: opt.RuntimeInstanceID, InputSHA256: b.InputSHA256,
		PolicySHA256: run.PolicySHA256, Status: model.ExecAllocated, LeaseUntil: &lease,
		OwnedDir: "ingest/" + b.RunID + "/" + tk.TaskID + "/" + "pending", StopReason: "",
		CreatedAt: now, UpdatedAt: now,
	}
	ex.OwnedDir = "ingest/" + b.RunID + "/" + tk.TaskID + "/" + ex.ExecutionID
	if barrier {
		ex.Status = model.ExecWaitingResource
	}
	if err := tx.InsertIngestExecution(ctx, ex); err != nil {
		return model.IngestExecution{}, err
	}
	if err := tx.InsertExecutionControl(ctx, model.ExecutionControl{ExecutionID: ex.ExecutionID, ControlVersion: 1, UpdatedAt: now}); err != nil {
		return model.IngestExecution{}, err
	}
	if !barrier {
		if err := tx.UpsertExecutionResource(ctx, model.ExecutionResource{
			ResourceKey: key, OwnerExecutionID: ex.ExecutionID, OwnerGeneration: ex.Generation,
			Strategy: model.ResourceRejectIfBusy, State: model.ResourceHeld, UpdatedAt: now,
		}); err != nil {
			return model.IngestExecution{}, err
		}
	}
	b.ExecutionID, b.ExecutionSeq = ex.ExecutionID, ex.Generation
	b.ExecutionState = ex.Status
	if barrier {
		b.HandoverState = "waiting_drain"
	} else {
		b.HandoverState = "granted"
	}
	if err := tx.UpdateIngestBinding(ctx, b); err != nil {
		return model.IngestExecution{}, err
	}
	if err := tx.InsertExecutionRequest(ctx, model.ExecutionRequest{
		RuntimeInstanceID: opt.RuntimeInstanceID, RequestID: opt.RequestID, Operation: opClaim,
		AgentID: agentID, ExecutionID: ex.ExecutionID, CreatedAt: now,
	}); err != nil {
		return model.IngestExecution{}, err
	}
	if err := tx.UpsertWorkerInstance(ctx, model.WorkerInstance{
		AgentID: agentID, Role: role, RuntimeInstanceID: opt.RuntimeInstanceID, Status: "active", ObservedAt: now,
	}); err != nil {
		return model.IngestExecution{}, err
	}
	if run.State == ingest.StateQueued {
		prev := run.Version
		run.State = ingest.StateProcessing
		run.Version++
		if err := tx.UpdateIngestRun(ctx, run, prev); err != nil {
			return model.IngestExecution{}, err
		}
	}
	return ex, nil
}

func outcomeFromExecution(ex model.IngestExecution, tk model.Task, version int, obsolete bool) ClaimOutcome {
	now := time.Now().UTC()
	out := ClaimOutcome{
		Claimed: !obsolete, Obsolete: obsolete, Scope: scopeFromExecution(ex), ControlVersion: version,
		ExecutionProtocol: ExecutionProtocolVersion,
	}
	if !obsolete {
		out.Task = tk
		out.LeaseExpiresAt = tk.LeaseUntil
		if tk.LeaseUntil != nil && tk.LeaseUntil.After(now) {
			out.LeaseRemaining = tk.LeaseUntil.Sub(now)
		}
	}
	return out
}

// BeginExecution marks the current execution runnable only after the previous
// owner has drained and the server resource barrier is clear.
func (s *Service) BeginExecution(ctx context.Context, scope ExecutionScope) error {
	return s.WithExecutionTx(ctx, scope, opBegin, func(ctx context.Context, tx *store.Store, auth executionAuth) error {
		key := sourceSnapshotKey(auth.Run.SourceID)
		res, err := tx.GetExecutionResource(ctx, key)
		if errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("execution has no resource grant: %w", model.ErrConflict)
		}
		if err != nil {
			return err
		}
		if res.Barrier || res.State == model.ResourceBlocked || res.State == model.ResourceDraining {
			return fmt.Errorf("previous execution has not drained: %w", model.ErrConflict)
		}
		if res.OwnerExecutionID != "" && res.OwnerExecutionID != auth.Execution.ExecutionID {
			prev, err := tx.GetIngestExecution(ctx, res.OwnerExecutionID)
			if err == nil && !executionTerminal(prev.Status) {
				return fmt.Errorf("resource owner has not drained: %w", model.ErrResourceBusy)
			}
			if err != nil && !errors.Is(err, model.ErrNotFound) {
				return err
			}
		}
		now := time.Now().UTC()
		res.OwnerExecutionID, res.OwnerGeneration = auth.Execution.ExecutionID, auth.Execution.Generation
		res.State, res.Barrier = model.ResourceHeld, false
		res.Strategy, res.UpdatedAt = model.ResourceRejectIfBusy, now
		if err := tx.UpsertExecutionResource(ctx, res); err != nil {
			return err
		}
		auth.Execution.Status = model.ExecRunning
		auth.Execution.LeaseUntil = auth.Task.LeaseUntil
		if err := tx.UpdateIngestExecution(ctx, auth.Execution); err != nil {
			return err
		}
		auth.Binding.ExecutionState = model.ExecRunning
		auth.Binding.HandoverState = "ready"
		return tx.UpdateIngestBinding(ctx, auth.Binding)
	})
}

// ExecutionControl is the authenticated poll body for one execution.
type ExecutionControl struct {
	ExecutionID    string `json:"execution_id"`
	Generation     int    `json:"generation"`
	ControlVersion int    `json:"control_version"`
	Command        string `json:"command"`
	Reason         string `json:"reason,omitempty"`
	Status         string `json:"status"`
	DrainRequired  bool   `json:"drain_required"`
}

func (s *Service) readExecutionControl(ctx context.Context, scope ExecutionScope) (ExecutionControl, error) {
	var view ExecutionControl
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		auth, err := authorizeExecution(ctx, tx, scope, opControl)
		if err != nil {
			return err
		}
		ctrl, err := tx.GetExecutionControl(ctx, auth.Execution.ExecutionID)
		if err != nil {
			return err
		}
		cmd := ctrl.Command
		if cmd == "" {
			cmd = "none"
		}
		view = ExecutionControl{
			ExecutionID: auth.Execution.ExecutionID, Generation: auth.Execution.Generation,
			ControlVersion: ctrl.ControlVersion, Command: cmd, Reason: ctrl.Reason,
			Status: auth.Execution.Status, DrainRequired: auth.Execution.Status == model.ExecStopRequested || auth.Execution.Status == model.ExecDraining || auth.Execution.Status == model.ExecCleanupBlocked,
		}
		return nil
	})
	return view, err
}

// GetExecutionControl polls the persisted control record. At most one poll
// waits per execution, and the wait is outside any SQLite transaction.
func (s *Service) GetExecutionControl(ctx context.Context, scope ExecutionScope, knownVersion int) (ExecutionControl, error) {
	cur, err := s.readExecutionControl(ctx, scope)
	if err != nil || cur.ControlVersion > knownVersion {
		return cur, err
	}
	sl, err := s.beginControlWait(scope.ExecutionID)
	if err != nil {
		return ExecutionControl{}, err
	}
	defer s.endControlWait(scope.ExecutionID)
	s.ctrlMu.Lock()
	hook := s.controlHook
	s.ctrlMu.Unlock()
	if hook != nil {
		hook()
	}
	timer := time.NewTimer(s.controlPollWait())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ExecutionControl{}, ctx.Err()
	case <-timer.C:
	case <-sl.notify:
	}
	return s.readExecutionControl(ctx, scope)
}

// AckExecutionStopped lets the original instance confirm stop or report that
// cleanup is blocked. It does not modify the replacement task.
func (s *Service) AckExecutionStopped(ctx context.Context, scope ExecutionScope, outcome string) error {
	if outcome != "stopped" && outcome != "cleanup_blocked" {
		return fmt.Errorf("outcome must be stopped or cleanup_blocked: %w", model.ErrArgument)
	}
	return s.WithExecutionTx(ctx, scope, opAck, func(ctx context.Context, tx *store.Store, auth executionAuth) error {
		now := time.Now().UTC()
		if auth.Execution.Status == model.ExecStopped && outcome == "stopped" {
			return nil
		}
		if auth.Execution.Status == model.ExecCleanupBlocked && outcome == "cleanup_blocked" {
			return nil
		}
		if auth.Execution.Status == model.ExecCleanupBlocked {
			return fmt.Errorf("cleanup is blocked until the owner execution is resolved: %w", model.ErrConflict)
		}
		if auth.Execution.Status != model.ExecStopRequested && auth.Execution.Status != model.ExecDraining && auth.Execution.Status != model.ExecStopped {
			return fmt.Errorf("execution has no stop command: %w", model.ErrInvalidState)
		}
		key := sourceSnapshotKey(auth.Run.SourceID)
		res, err := tx.GetExecutionResource(ctx, key)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if outcome == "cleanup_blocked" {
			auth.Execution.Status = model.ExecCleanupBlocked
			if err := tx.UpdateIngestExecution(ctx, auth.Execution); err != nil {
				return err
			}
			if res.ResourceKey == "" {
				res.ResourceKey = key
			}
			if res.OwnerExecutionID == "" {
				res.OwnerExecutionID, res.OwnerGeneration = auth.Execution.ExecutionID, auth.Execution.Generation
			}
			res.State, res.Barrier = model.ResourceBlocked, true
			res.UpdatedAt = now
			if err := tx.UpsertExecutionResource(ctx, res); err != nil {
				return err
			}
			return tx.AckExecutionControl(ctx, auth.Execution.ExecutionID, outcome, now)
		}
		auth.Execution.Status = model.ExecStopped
		auth.Execution.DrainedAt = &now
		if err := tx.UpdateIngestExecution(ctx, auth.Execution); err != nil {
			return err
		}
		if res.OwnerExecutionID == auth.Execution.ExecutionID {
			res.OwnerExecutionID, res.OwnerGeneration = "", 0
			res.State, res.Barrier = model.ResourceReleased, false
			res.UpdatedAt = now
			if err := tx.UpsertExecutionResource(ctx, res); err != nil {
				return err
			}
		}
		return tx.AckExecutionControl(ctx, auth.Execution.ExecutionID, outcome, now)
	})
}

// ResolveCleanupBlocked releases a blocked resource after the server checks
// that the named execution is the owner. It accepts no pid and no path.
func (s *Service) ResolveCleanupBlocked(ctx context.Context, actor, executionID string) error {
	if strings.TrimSpace(actor) == "" || strings.HasPrefix(actor, "agent:") {
		return fmt.Errorf("only a human actor can resolve a blocked cleanup: %w", model.ErrForbidden)
	}
	if !idemKeyPattern.MatchString(executionID) {
		return fmt.Errorf("execution_id is invalid: %w", model.ErrArgument)
	}
	return s.st.Transaction(ctx, func(tx *store.Store) error {
		ex, err := tx.GetIngestExecution(ctx, executionID)
		if err != nil {
			return err
		}
		if ex.Status != model.ExecCleanupBlocked {
			return fmt.Errorf("execution is %s: %w", ex.Status, model.ErrInvalidState)
		}
		run, err := tx.GetIngestRun(ctx, ex.RunID)
		if err != nil {
			return err
		}
		key := sourceSnapshotKey(run.SourceID)
		res, err := tx.GetExecutionResource(ctx, key)
		if err != nil {
			return err
		}
		if res.OwnerExecutionID != executionID {
			return fmt.Errorf("resource owner does not match this execution: %w", model.ErrConflict)
		}
		now := time.Now().UTC()
		ex.Status = model.ExecStopped
		ex.DrainedAt = &now
		if err := tx.UpdateIngestExecution(ctx, ex); err != nil {
			return err
		}
		res.OwnerExecutionID, res.OwnerGeneration = "", 0
		res.State, res.Barrier, res.UpdatedAt = model.ResourceReleased, false, now
		if err := tx.UpsertExecutionResource(ctx, res); err != nil {
			return err
		}
		return auditTx(ctx, tx, actor, "execution.cleanup.resolve", executionID, "owner matched")
	})
}

// ExecutionResultStatus is the committed/not-committed answer for one known request.
type ExecutionResultStatus struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

// GetExecutionResultStatus reads only the caller's own submit request.
func (s *Service) GetExecutionResultStatus(ctx context.Context, scope ExecutionScope, requestID string) (ExecutionResultStatus, error) {
	if !idemKeyPattern.MatchString(requestID) {
		return ExecutionResultStatus{}, fmt.Errorf("request_id is invalid: %w", model.ErrArgument)
	}
	var out ExecutionResultStatus
	err := s.WithExecutionTx(ctx, scope, opResult, func(ctx context.Context, tx *store.Store, auth executionAuth) error {
		req, err := tx.GetExecutionRequest(ctx, scope.RuntimeInstanceID, requestID, opSubmit)
		if errors.Is(err, model.ErrNotFound) {
			out = ExecutionResultStatus{RequestID: requestID, Status: "not_committed"}
			return nil
		}
		if err != nil {
			return err
		}
		if req.AgentID != scope.AgentID || req.ExecutionID != scope.ExecutionID {
			return fmt.Errorf("request belongs to another execution: %w", model.ErrForbidden)
		}
		switch req.ResultStatus {
		case "committed":
			out = ExecutionResultStatus{RequestID: requestID, Status: "committed"}
		case "conflict":
			out = ExecutionResultStatus{RequestID: requestID, Status: "conflict"}
		case "obsolete":
			out = ExecutionResultStatus{RequestID: requestID, Status: "obsolete"}
		default:
			if auth.Execution.Status == model.ExecSucceeded {
				out = ExecutionResultStatus{RequestID: requestID, Status: "committed"}
			} else {
				out = ExecutionResultStatus{RequestID: requestID, Status: "not_committed"}
			}
		}
		return nil
	})
	return out, err
}

// RecoveryCandidate is a bounded, path-free pointer at an earlier execution
// of the same run and fixed input.
type RecoveryCandidate struct {
	FormerExecutionID string `json:"former_execution_id"`
	Generation        int    `json:"generation"`
	Stage             string `json:"stage"`
	InputSHA256       string `json:"input_sha256"`
	CheckpointCount   int    `json:"checkpoint_count"`
	Summary           string `json:"summary,omitempty"`
	TaskID            string `json:"task_id"`
	OutputDir         string `json:"output_dir"`
	PackageDir        string `json:"package_dir"`
	JournalRef        string `json:"journal_ref"`
	PolicySHA256      string `json:"policy_sha256"`
}

// ListRecoveryCandidates returns earlier executions only to the current owner.
func (s *Service) ListRecoveryCandidates(ctx context.Context, scope ExecutionScope) ([]RecoveryCandidate, error) {
	var out []RecoveryCandidate
	err := s.WithExecutionTx(ctx, scope, opRecovery, func(ctx context.Context, tx *store.Store, auth executionAuth) error {
		rows, err := tx.ListIngestExecutionsByTask(ctx, auth.Task.TaskID)
		if err != nil {
			return err
		}
		bindings, err := tx.ListIngestBindings(ctx, auth.Run.RunID)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			if b.Stage != auth.Binding.Stage || b.InputSHA256 != auth.Binding.InputSHA256 {
				continue
			}
			more, err := tx.ListIngestExecutionsByTask(ctx, b.TaskID)
			if err != nil {
				return err
			}
			rows = append(rows, more...)
		}
		seen := map[string]bool{}
		for _, ex := range rows {
			if seen[ex.ExecutionID] || ex.ExecutionID == auth.Execution.ExecutionID || ex.Generation >= auth.Execution.Generation && ex.TaskID == auth.Task.TaskID {
				continue
			}
			if ex.InputSHA256 != auth.Binding.InputSHA256 || ex.PolicySHA256 != auth.Run.PolicySHA256 || ex.RunID != auth.Run.RunID {
				continue
			}
			if ex.Status != model.ExecStopped && ex.Status != model.ExecSucceeded && ex.Status != model.ExecFailed {
				continue
			}
			seen[ex.ExecutionID] = true
			cps, err := tx.ListCheckpoints(ctx, ex.TaskID)
			if err != nil {
				return err
			}
			n := 0
			for _, cp := range cps {
				if cp.ExecutionID == ex.ExecutionID {
					n++
				}
			}
			out = append(out, RecoveryCandidate{
				FormerExecutionID: ex.ExecutionID, Generation: ex.Generation, Stage: auth.Binding.Stage,
				InputSHA256: ex.InputSHA256, CheckpointCount: n, Summary: "same-run fixed input",
				TaskID: ex.TaskID, OutputDir: ex.OwnedDir, PackageDir: ex.OwnedDir + "/package",
				JournalRef: ex.OwnedDir + "/journal.v1.json", PolicySHA256: ex.PolicySHA256,
			})
			if len(out) == 20 {
				break
			}
		}
		if out == nil {
			out = []RecoveryCandidate{}
		}
		return nil
	})
	return out, err
}

func (s *Service) guardResourceBeforeQueue(ctx context.Context, tx *store.Store, run model.IngestRun, actor, policy string) (bool, error) {
	policy, err := normalizeResourcePolicy(policy)
	if err != nil {
		return false, err
	}
	if err := rejectAgentPreempt(actor, policy); err != nil {
		return false, err
	}
	key := sourceSnapshotKey(run.SourceID)
	res, err := tx.GetExecutionResource(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !resourceOccupied(res) {
		return false, nil
	}
	if policy == model.ResourceRejectIfBusy {
		return false, fmt.Errorf("an execution holds this recording: %w", model.ErrResourceBusy)
	}
	return true, nil
}
