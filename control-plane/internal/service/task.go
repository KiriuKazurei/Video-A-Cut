package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// CreateTask queues one new unit of work on an existing asset.
//
// The caller may only name what should happen and who may do it: the status is
// forced to queued because a freshly created task has not been picked up yet,
// and letting a caller choose the starting status would let a task skip the
// queue entirely. The asset is confirmed to exist first so the task can never
// reference a dangling asset — the store has no such constraint beyond the
// foreign key being enabled per connection.
//
// An empty TaskID, AssetID or AgentRole is rejected with model.ErrArgument
// before any store call, and a missing asset surfaces model.ErrNotFound from
// the existence check. On success the task is announced as task_created and
// recorded in the audit log under the "system" actor, because a task is
// created by the pipeline rather than by a human.
func (s *Service) CreateTask(ctx context.Context, tk model.Task) error {
	if !validTaskID(tk.TaskID) {
		return fmt.Errorf("service: create task: task_id must be a non-empty URL path segment: %w", model.ErrArgument)
	}
	if tk.AssetID == "" {
		return fmt.Errorf("service: create task %s: asset_id is required: %w", tk.TaskID, model.ErrArgument)
	}
	if tk.AgentRole == "" {
		return fmt.Errorf("service: create task %s: agent_role is required: %w", tk.TaskID, model.ErrArgument)
	}
	// Type is the requested execution operation, while ClaimTask selects by
	// AgentRole. Requiring a non-empty Type ensures a queued task still carries
	// an operation specification for the worker that claims it. Validation
	// stays here so every future transport shares the same rule.
	if tk.Type == "" {
		return fmt.Errorf("service: create task %s: type is required: %w", tk.TaskID, model.ErrArgument)
	}
	if ingest.IsTaskType(tk.Type) || tk.AgentRole == ingest.RoleIngester {
		return fmt.Errorf("service: create task %s: ingest tasks are created only by ingest runs: %w", tk.TaskID, model.ErrArgument)
	}

	// A task starts as a clean queue entry regardless of fields a direct
	// service caller may have pre-populated. The HTTP request type is closed,
	// but service is the business boundary used by every future transport.
	tk.Status = model.TaskStatusQueued
	tk.AgentID = ""
	tk.Progress = 0
	tk.Message = ""
	tk.LeaseUntil = nil
	tk.ClaimedAt = nil
	tk.Artifacts = map[string]string{}
	tk.Attempts = 0
	deps, err := normalizeDependsOn(tk.TaskID, tk.DependsOn)
	if err != nil {
		return err
	}
	tk.DependsOn = deps
	var saved model.Task
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if _, err := tx.GetAsset(ctx, tk.AssetID); err != nil {
			return fmt.Errorf("service: create task %s: %w", tk.TaskID, err)
		}
		// Dependencies must already exist, so the graph can only grow by
		// pointing at older tasks: a cycle is impossible by construction.
		// They must also belong to the same asset; cross-asset ordering is
		// a scheduling policy this control plane does not own.
		for _, dep := range tk.DependsOn {
			d, err := tx.GetTask(ctx, dep)
			if err != nil {
				return fmt.Errorf("service: create task %s: dependency %s: %w", tk.TaskID, dep, err)
			}
			if d.AssetID != tk.AssetID {
				return fmt.Errorf("service: create task %s: dependency %s belongs to another asset: %w",
					tk.TaskID, dep, model.ErrArgument)
			}
			if d.Status == model.TaskStatusFailed || d.Status == model.TaskStatusCancelled {
				return fmt.Errorf("service: create task %s: dependency %s is %s: %w",
					tk.TaskID, dep, d.Status, model.ErrInvalidState)
			}
		}
		if err := tx.CreateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: create task %s: %w", tk.TaskID, err)
		}
		var err error
		saved, err = tx.GetTask(ctx, tk.TaskID)
		return err
	})
	if err != nil {
		return err
	}

	s.publish("task_created", cloneTask(saved))
	s.audit(ctx, "system", "task.create", tk.TaskID, "")
	return nil
}

// GetTask returns one task by id. It is a thin pass-through: transport
// handlers need the raw task to render a job view, and every mutation goes
// through its own method that owns the state machine.
func (s *Service) GetTask(ctx context.Context, id string) (model.Task, error) {
	return s.st.GetTask(ctx, id)
}

// ClaimTask hands the oldest queued task of a role to one agent under a lease.
//
// The lease is the whole point: a task handed to an agent with no expiry would
// be stuck forever if that agent died mid-flight, which is exactly the failure
// the recovery sweep exists to repair.
//
// The idempotency rule runs first, before the queue is consulted: an agent that
// already holds a live lease gets that task back instead of being handed a
// second one. A disconnecting client that reconnects and re-claims must not
// accumulate work it is already doing, and must not steal another agent's task
// either — holding a live lease means the task still belongs to you.
//
// When nothing is queued the caller gets model.ErrNotFound, so a handler can
// tell "the queue is empty" (poll again later) apart from a real error.
// ClaimTask claims ordinary work, or an ingest task when the caller has no
// instance identity. Ingest tasks then fail closed inside the transaction
// rather than being handed out without a scope.
func (s *Service) ClaimTask(ctx context.Context, agentID, role string, lease time.Time) (model.Task, error) {
	out, err := s.ClaimForExecution(ctx, agentID, role, lease, ClaimOptions{})
	if err != nil {
		return model.Task{}, err
	}
	if out.Obsolete {
		return model.Task{}, fmt.Errorf("claim request is obsolete: %w", model.ErrObsolete)
	}
	return out.Task, nil
}

// ClaimForExecution claims a task and, for ingest work, allocates an execution
// scope. Replaying request_id returns the original scope or obsolete.
func (s *Service) ClaimForExecution(ctx context.Context, agentID, role string, lease time.Time, opt ClaimOptions) (ClaimOutcome, error) {
	if agentID == "" {
		return ClaimOutcome{}, fmt.Errorf("service: claim task: agent_id is required: %w", model.ErrArgument)
	}
	if role == "" {
		return ClaimOutcome{}, fmt.Errorf("service: claim task: agent_role is required: %w", model.ErrArgument)
	}

	var outcome ClaimOutcome
	changed := false
	var wake []string
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		now := time.Now().UTC()
		if !validLeaseUntil(lease, now) {
			return fmt.Errorf("service: claim task: lease expiry must be in the future: %w", model.ErrArgument)
		}
		if opt.RequestID != "" || opt.RuntimeInstanceID != "" {
			if !idemKeyPattern.MatchString(opt.RuntimeInstanceID) || !idemKeyPattern.MatchString(opt.RequestID) {
				return fmt.Errorf("runtime_instance_id and request_id are required together: %w", model.ErrArgument)
			}
			req, err := tx.GetExecutionRequest(ctx, opt.RuntimeInstanceID, opt.RequestID, opClaim)
			if err == nil {
				ex, err := tx.GetIngestExecution(ctx, req.ExecutionID)
				if err != nil {
					return err
				}
				tk, err := tx.GetTask(ctx, ex.TaskID)
				if err != nil {
					return err
				}
				ctrl, err := tx.GetExecutionControl(ctx, ex.ExecutionID)
				version := 0
				if err == nil {
					version = ctrl.ControlVersion
				} else if !errors.Is(err, model.ErrNotFound) {
					return err
				}
				b, berr := tx.GetIngestBinding(ctx, ex.TaskID)
				live := berr == nil && b.ExecutionID == ex.ExecutionID && executionCurrent(ex.Status)
				outcome = outcomeFromExecution(ex, tk, version, !live)
				return nil
			} else if !errors.Is(err, model.ErrNotFound) {
				return err
			}
		}
		active, err := activeTasksForAgent(ctx, tx, agentID)
		if err != nil {
			return fmt.Errorf("service: claim task for agent %s: %w", agentID, err)
		}
		for _, tk := range active {
			if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
				continue
			}
			if tk.LeaseUntil == nil || !tk.LeaseUntil.After(now) {
				continue
			}
			if tk.AgentRole != role {
				return fmt.Errorf("service: claim task %s: agent role %q does not match held task role %q: %w",
					tk.TaskID, role, tk.AgentRole, model.ErrForbidden)
			}
			asset, err := tx.GetAsset(ctx, tk.AssetID)
			if err != nil {
				return fmt.Errorf("service: claim task %s asset: %w", tk.TaskID, err)
			}
			if !assetVisibleForRole(asset, role) {
				return fmt.Errorf("service: claim task %s: asset is not visible to role %q: %w",
					tk.TaskID, role, model.ErrForbidden)
			}
			if b, err := tx.GetIngestBinding(ctx, tk.TaskID); err == nil {
				if b.ExecutionID == "" {
					return fmt.Errorf("held ingest task has no execution: %w", model.ErrConflict)
				}
				ex, err := tx.GetIngestExecution(ctx, b.ExecutionID)
				if err != nil {
					return err
				}
				if opt.RuntimeInstanceID == "" || ex.RuntimeInstanceID != opt.RuntimeInstanceID {
					return fmt.Errorf("another runtime instance holds this execution: %w", model.ErrInstanceConflict)
				}
				if opt.RequestID != "" {
					if _, err := tx.GetExecutionRequest(ctx, opt.RuntimeInstanceID, opt.RequestID, opClaim); errors.Is(err, model.ErrNotFound) {
						if err := tx.InsertExecutionRequest(ctx, model.ExecutionRequest{
							RuntimeInstanceID: opt.RuntimeInstanceID, RequestID: opt.RequestID, Operation: opClaim,
							AgentID: agentID, ExecutionID: ex.ExecutionID, CreatedAt: now,
						}); err != nil {
							return err
						}
					} else if err != nil {
						return err
					}
				}
				ctrl, _ := tx.GetExecutionControl(ctx, ex.ExecutionID)
				outcome = outcomeFromExecution(ex, cloneTask(tk), ctrl.ControlVersion, false)
				return nil
			} else if !errors.Is(err, model.ErrNotFound) {
				return err
			}
			outcome = ClaimOutcome{Claimed: true, Task: cloneTask(tk), LeaseExpiresAt: tk.LeaseUntil}
			if tk.LeaseUntil != nil {
				outcome.LeaseRemaining = tk.LeaseUntil.Sub(now)
			}
			return nil
		}

		candidates, err := tx.ClaimCandidates(ctx, role)
		if err != nil {
			return fmt.Errorf("service: claim task for role %s: %w", role, err)
		}
		for _, candidate := range candidates {
			if err := s.WorkflowBlocks(ctx, tx, candidate.TaskID); err != nil {
				continue
			}
			if ok, err := s.ingestClaimable(ctx, tx, agentID, candidate); err != nil {
				return err
			} else if !ok {
				continue
			}
			if stage, e := tx.StageByTask(ctx, candidate.TaskID); e == nil {
				if binding, e := tx.ProfileBinding(ctx, stage.RunID); e == nil {
					ok, e := tx.HasCapability(ctx, agentID, binding.SHA256)
					if e != nil {
						return e
					}
					if !ok {
						continue
					}
				} else if !errors.Is(e, model.ErrNotFound) {
					return e
				}
			} else if !errors.Is(e, model.ErrNotFound) {
				return e
			}
			asset, err := tx.GetAsset(ctx, candidate.AssetID)
			if err != nil {
				return fmt.Errorf("service: claim task %s asset: %w", candidate.TaskID, err)
			}
			if !assetVisibleForRole(asset, role) {
				continue
			}
			ready, err := dependenciesReady(ctx, tx, candidate)
			if err != nil {
				return fmt.Errorf("service: claim task %s dependencies: %w", candidate.TaskID, err)
			}
			if !ready || !typeReady(candidate, asset) {
				continue
			}

			candidate.Status = model.TaskStatusClaimed
			candidate.AgentID = agentID
			candidate.ClaimedAt = &now
			candidate.LeaseUntil = &lease
			candidate.Progress = 0
			candidate.Message = ""
			candidate.Artifacts = map[string]string{}
			if err := tx.UpdateTask(ctx, candidate); err != nil {
				return fmt.Errorf("service: claim task %s: %w", candidate.TaskID, err)
			}
			if _, err := tx.GetIngestBinding(ctx, candidate.TaskID); err == nil {
				ex, err := s.allocateIngestExecution(ctx, tx, agentID, role, opt, candidate, lease, &wake)
				if err != nil {
					return fmt.Errorf("service: claim task %s: %w", candidate.TaskID, err)
				}
				saved, err := tx.GetTask(ctx, candidate.TaskID)
				if err != nil {
					return err
				}
				outcome = outcomeFromExecution(ex, saved, 1, false)
			} else if !errors.Is(err, model.ErrNotFound) {
				return err
			} else {
				saved, err := tx.GetTask(ctx, candidate.TaskID)
				if err != nil {
					return fmt.Errorf("service: read claimed task %s: %w", candidate.TaskID, err)
				}
				outcome = ClaimOutcome{Claimed: true, Task: saved, LeaseExpiresAt: saved.LeaseUntil}
				if saved.LeaseUntil != nil {
					outcome.LeaseRemaining = saved.LeaseUntil.Sub(now)
				}
			}
			changed = true
			return nil
		}
		return fmt.Errorf("service: claim task for role %s: nothing visible queued: %w", role, model.ErrNotFound)
	})
	if err != nil {
		return ClaimOutcome{}, err
	}
	s.finishControl(wake)
	if changed {
		s.publish("task_updated", cloneTask(outcome.Task))
		s.audit(ctx, "agent:"+agentID, "task.claim", outcome.Task.TaskID, "")
	}
	outcome.Task = cloneTask(outcome.Task)
	return outcome, nil
}

// ReportProgress records an agent's progress on a task it owns.
//
// A report is the agent's own heartbeat against the lease, so an expired lease
// is refused with model.ErrLeaseExpired: the recovery sweep may already have
// handed the task away, and letting the stale agent keep writing would
// overwrite the new holder's state. Progress is clamped into [0, 1] so a
// mis-scaled client cannot report more than a full job.
//
// The first report on a claimed task moves it to running. Terminal tasks are
// refused with model.ErrInvalidState — a succeeded or failed job must not be
// reopened by a late heartbeat.
//
// No audit entry is written: progress is high-frequency telemetry, and an
// audit row per heartbeat would bury the governance events that actually
// matter.
func (s *Service) ReportProgress(ctx context.Context, agentID, taskID string, progress float64, message string) error {
	return s.ReportProgressScoped(ctx, ExecutionScope{AgentID: agentID, TaskID: taskID}, progress, message)
}

// ReportProgressScoped records progress for a historical task, or for the
// current begun ingest execution identified by scope.
func (s *Service) ReportProgressScoped(ctx context.Context, scope ExecutionScope, progress float64, message string) error {
	if math.IsNaN(progress) || math.IsInf(progress, 0) {
		return fmt.Errorf("service: report progress on task %s: progress must be finite: %w", scope.TaskID, model.ErrArgument)
	}
	var saved model.Task
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		tk, err := guardTaskMutation(ctx, tx, scope.AgentID, scope.TaskID)
		if err != nil {
			return err
		}
		if _, err := authorizeExecution(ctx, tx, scope, opProgress); err != nil {
			return err
		}
		if leaseExpired(tk) {
			return fmt.Errorf("service: report progress on task %s: %w", scope.TaskID, model.ErrLeaseExpired)
		}
		if err := s.WorkflowBlocks(ctx, tx, scope.TaskID); err != nil {
			return err
		}
		if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
			return fmt.Errorf("service: report progress on task %s: status %q is not active: %w",
				scope.TaskID, tk.Status, model.ErrInvalidState)
		}
		if tk.Status == model.TaskStatusClaimed {
			tk.Status = model.TaskStatusRunning
		}
		tk.Progress = ingestProgress(ctx, tx, tk, clampProgress(progress))
		if message != "" {
			tk.Message = message
		}
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: report progress on task %s: %w", scope.TaskID, err)
		}
		saved, err = tx.GetTask(ctx, scope.TaskID)
		return err
	})
	if err != nil {
		return err
	}

	s.publish("task_updated", cloneTask(saved))
	return nil
}

// SubmitResult records the artifacts of a finished task and closes it.
//
// Submitting twice is deliberately not an error: an agent that never saw the
// response to its first submit retries, and failing that retry would make the
// agent report success on a task that is still in flight from the control
// plane's point of view. The second call returns nil immediately and changes
// nothing. Anything that is not claimed or running is refused with
// model.ErrInvalidState, because a terminal task cannot be overwritten by a
// late submission, and an expired lease with model.ErrLeaseExpired.
func (s *Service) SubmitResult(ctx context.Context, agentID, taskID string, artifacts map[string]string) error {
	var saved model.Task
	changed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		tk, err := guardTaskMutation(ctx, tx, agentID, taskID)
		if err != nil {
			return err
		}
		if _, err := tx.StageByTask(ctx, taskID); err == nil {
			return fmt.Errorf("workflow tasks require submit_delivery: %w", model.ErrInvalidState)
		}
		if _, err := tx.GetIngestBinding(ctx, taskID); err == nil {
			return fmt.Errorf("ingest tasks require submit_ingest_result: %w", model.ErrInvalidState)
		}
		if tk.Status == model.TaskStatusSucceeded {
			return nil
		}
		if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
			return fmt.Errorf("service: submit result on task %s: status %q is not active: %w",
				taskID, tk.Status, model.ErrInvalidState)
		}
		if leaseExpired(tk) {
			return fmt.Errorf("service: submit result on task %s: %w", taskID, model.ErrLeaseExpired)
		}

		tk.Status = model.TaskStatusSucceeded
		tk.Progress = 1
		tk.Artifacts = cloneStringMap(artifacts)
		tk.LeaseUntil = nil
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: submit result on task %s: %w", taskID, err)
		}
		saved, err = tx.GetTask(ctx, taskID)
		changed = true
		return err
	})
	if err != nil {
		return err
	}
	if changed {
		s.publish("task_updated", cloneTask(saved))
		s.audit(ctx, "agent:"+agentID, "task.submit", taskID, "")
	}
	return nil
}

// FailTask closes a task as failed with the reason the agent gave.
//
// The agent has to be the current holder — another agent failing a task it
// does not own would be a way to unqueue somebody else's work. The reason is
// kept in the message column and recorded as the audit detail so a failure is
// explainable after the fact.
//
// A task that already succeeded is refused with model.ErrInvalidState: the
// work is done and its artifacts are recorded, so retroactively failing it
// would discard output that downstream steps depend on. Failing an already
// failed task is accepted and simply rewrites the reason.
func (s *Service) FailTask(ctx context.Context, agentID, taskID, reason string) error {
	return s.FailTaskScoped(ctx, ExecutionScope{AgentID: agentID, TaskID: taskID}, reason)
}

// FailTaskScoped closes the current execution. A revoked execution cannot
// fail the task that replaced it.
func (s *Service) FailTaskScoped(ctx context.Context, scope ExecutionScope, reason string) error {
	var saved model.Task
	changed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		tk, err := guardTaskMutation(ctx, tx, scope.AgentID, scope.TaskID)
		if err != nil {
			return err
		}
		auth, err := authorizeExecution(ctx, tx, scope, opFail)
		if err != nil {
			return err
		}
		// Repeating a failed outcome is idempotent and cannot rewrite its
		// message or audit record. Other terminal and queued states cannot
		// be converted into failures by a late agent request.
		if tk.Status == model.TaskStatusFailed {
			return nil
		}
		if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
			return fmt.Errorf("service: fail task %s: status %q cannot fail: %w",
				scope.TaskID, tk.Status, model.ErrInvalidState)
		}
		if leaseExpired(tk) {
			return fmt.Errorf("service: fail task %s: %w", scope.TaskID, model.ErrLeaseExpired)
		}
		if err := s.WorkflowBlocks(ctx, tx, scope.TaskID); err != nil {
			return err
		}

		tk.Status = model.TaskStatusFailed
		tk.Message = reason
		tk.LeaseUntil = nil
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: fail task %s: %w", scope.TaskID, err)
		}
		if !auth.Historical {
			auth.Execution.Status = model.ExecFailed
			auth.Execution.StopReason = sanitizeReason(reason)
			if err := tx.UpdateIngestExecution(ctx, auth.Execution); err != nil {
				return err
			}
			if err := s.releaseResourceIfOwner(ctx, tx, auth.Run, auth.Execution.ExecutionID); err != nil {
				return err
			}
		}
		if err := s.NoteWorkflowFailure(ctx, tx, tk, reason); err != nil {
			return err
		}
		if err := s.NoteIngestFailure(ctx, tx, tk, reason); err != nil {
			return err
		}
		saved, err = tx.GetTask(ctx, scope.TaskID)
		changed = true
		return err
	})
	if err != nil {
		return err
	}
	if changed {
		s.publishTaskWorkflow(ctx, scope.TaskID)
		s.publishTaskIngest(ctx, scope.TaskID)
		s.publish("task_updated", cloneTask(saved))
		s.audit(ctx, "agent:"+scope.AgentID, "task.fail", scope.TaskID, reason)
		s.cascadeFailure(ctx, scope.TaskID)
	}
	return nil
}

// guardTaskMutation loads one task and applies the checks every mutating
// agent call shares: the task must exist, the caller must be its holder, and
// the check for an expired lease.
//
// AgentID ownership lives here rather than in the store because the store
// knows nothing about agents — an agent id is an identity, and identity rules
// belong to the service layer.
func guardTaskMutation(ctx context.Context, st *store.Store, agentID, taskID string) (model.Task, error) {
	if agentID == "" {
		return model.Task{}, fmt.Errorf("service: mutate task %s: agent_id is required: %w", taskID, model.ErrArgument)
	}

	tk, err := st.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, fmt.Errorf("service: mutate task %s: %w", taskID, err)
	}
	if tk.AgentID != agentID {
		return model.Task{}, fmt.Errorf("service: mutate task %s: agent %q is not the holder: %w",
			taskID, agentID, model.ErrForbidden)
	}
	return tk, nil
}

// activeTasksForAgent returns the non-terminal tasks held by one agent.
//
// It lists the active tasks once and filters by AgentID in memory rather than
// adding a second indexed query to the store: the active set is small, and the
// lease check needs a single definition of "active" shared with the recovery
// sweep.
func activeTasksForAgent(ctx context.Context, st *store.Store, agentID string) ([]model.Task, error) {
	all, err := st.ListActiveTasks(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]model.Task, 0, len(all))
	for _, tk := range all {
		if tk.AgentID == agentID {
			out = append(out, tk)
		}
	}
	return out, nil
}

// leaseExpired reports whether tk holds a lease that has already lapsed. A
// task with no lease at all — a queued one, or one cleared by a terminal
// transition — has nothing to expire.
func leaseExpired(tk model.Task) bool {
	return tk.LeaseUntil != nil && !tk.LeaseUntil.After(time.Now())
}

// clampProgress forces a caller-supplied progress value into [0, 1].
func clampProgress(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

func validLeaseUntil(lease, now time.Time) bool {
	return !lease.IsZero() && lease.After(now)
}

func validTaskID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func cloneTask(tk model.Task) model.Task {
	if tk.LeaseUntil != nil {
		v := *tk.LeaseUntil
		tk.LeaseUntil = &v
	}
	if tk.ClaimedAt != nil {
		v := *tk.ClaimedAt
		tk.ClaimedAt = &v
	}
	tk.Artifacts = cloneStringMap(tk.Artifacts)
	return tk
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
