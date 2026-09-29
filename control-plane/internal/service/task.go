package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

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
func (s *Service) ClaimTask(ctx context.Context, agentID, role string, lease time.Time) (model.Task, error) {
	if agentID == "" {
		return model.Task{}, fmt.Errorf("service: claim task: agent_id is required: %w", model.ErrArgument)
	}
	if role == "" {
		return model.Task{}, fmt.Errorf("service: claim task: agent_role is required: %w", model.ErrArgument)
	}

	var claimed model.Task
	changed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		now := time.Now().UTC()
		if !validLeaseUntil(lease, now) {
			return fmt.Errorf("service: claim task: lease expiry must be in the future: %w", model.ErrArgument)
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
			claimed = cloneTask(tk)
			return nil
		}

		candidates, err := tx.ClaimCandidates(ctx, role)
		if err != nil {
			return fmt.Errorf("service: claim task for role %s: %w", role, err)
		}
		for _, candidate := range candidates {
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
			claimed, err = tx.GetTask(ctx, candidate.TaskID)
			if err != nil {
				return fmt.Errorf("service: read claimed task %s: %w", candidate.TaskID, err)
			}
			changed = true
			return nil
		}
		return fmt.Errorf("service: claim task for role %s: nothing visible queued: %w", role, model.ErrNotFound)
	})
	if err != nil {
		return model.Task{}, err
	}
	if changed {
		s.publish("task_updated", cloneTask(claimed))
		s.audit(ctx, "agent:"+agentID, "task.claim", claimed.TaskID, "")
	}
	return cloneTask(claimed), nil
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
	if math.IsNaN(progress) || math.IsInf(progress, 0) {
		return fmt.Errorf("service: report progress on task %s: progress must be finite: %w", taskID, model.ErrArgument)
	}
	var saved model.Task
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		tk, err := guardTaskMutation(ctx, tx, agentID, taskID)
		if err != nil {
			return err
		}
		if leaseExpired(tk) {
			return fmt.Errorf("service: report progress on task %s: %w", taskID, model.ErrLeaseExpired)
		}
		if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
			return fmt.Errorf("service: report progress on task %s: status %q is not active: %w",
				taskID, tk.Status, model.ErrInvalidState)
		}
		if tk.Status == model.TaskStatusClaimed {
			tk.Status = model.TaskStatusRunning
		}
		tk.Progress = clampProgress(progress)
		if message != "" {
			tk.Message = message
		}
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: report progress on task %s: %w", taskID, err)
		}
		saved, err = tx.GetTask(ctx, taskID)
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
	var saved model.Task
	changed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		tk, err := guardTaskMutation(ctx, tx, agentID, taskID)
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
				taskID, tk.Status, model.ErrInvalidState)
		}
		if leaseExpired(tk) {
			return fmt.Errorf("service: fail task %s: %w", taskID, model.ErrLeaseExpired)
		}

		tk.Status = model.TaskStatusFailed
		tk.Message = reason
		tk.LeaseUntil = nil
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: fail task %s: %w", taskID, err)
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
		s.audit(ctx, "agent:"+agentID, "task.fail", taskID, reason)
		s.cascadeFailure(ctx, taskID)
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
