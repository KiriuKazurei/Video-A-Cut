package service

import (
	"context"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
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
	if tk.TaskID == "" {
		return fmt.Errorf("service: create task: task_id is required: %w", model.ErrArgument)
	}
	if tk.AssetID == "" {
		return fmt.Errorf("service: create task %s: asset_id is required: %w", tk.TaskID, model.ErrArgument)
	}
	if tk.AgentRole == "" {
		return fmt.Errorf("service: create task %s: agent_role is required: %w", tk.TaskID, model.ErrArgument)
	}
	// Type is validated here and not only at the transport, because it is
	// the field ClaimTask matches an agent on: an untyped task queues as
	// claimable work that no agent could ever claim, which reads as a
	// stall rather than a rejected request. A REST-only guard would leave
	// the same hole open for the MCP dispatch surface of Phase 3, which
	// reaches this method without passing through internal/api.
	if tk.Type == "" {
		return fmt.Errorf("service: create task %s: type is required: %w", tk.TaskID, model.ErrArgument)
	}

	if _, err := s.st.GetAsset(ctx, tk.AssetID); err != nil {
		return fmt.Errorf("service: create task %s: %w", tk.TaskID, err)
	}

	tk.Status = model.TaskStatusQueued
	if err := s.st.CreateTask(ctx, tk); err != nil {
		return fmt.Errorf("service: create task %s: %w", tk.TaskID, err)
	}

	s.publish("task_created", tk)
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

	now := time.Now().UTC()
	for _, tk := range s.activeTasksForAgent(ctx, agentID) {
		if tk.LeaseUntil != nil && tk.LeaseUntil.After(now) {
			return tk, nil
		}
	}

	candidates, err := s.st.ClaimCandidates(ctx, role)
	if err != nil {
		return model.Task{}, fmt.Errorf("service: claim task for role %s: %w", role, err)
	}
	if len(candidates) == 0 {
		return model.Task{}, fmt.Errorf("service: claim task for role %s: nothing queued: %w", role, model.ErrNotFound)
	}

	tk := candidates[0]
	tk.Status = model.TaskStatusClaimed
	tk.AgentID = agentID
	tk.AgentRole = role
	tk.ClaimedAt = &now
	tk.LeaseUntil = &lease
	tk.Progress = 0
	if err := s.st.UpdateTask(ctx, tk); err != nil {
		return model.Task{}, fmt.Errorf("service: claim task %s: %w", tk.TaskID, err)
	}

	s.publish("task_updated", tk)
	s.audit(ctx, "agent:"+agentID, "task.claim", tk.TaskID, "")
	return tk, nil
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
	tk, err := s.guardTaskMutation(ctx, agentID, taskID)
	if err != nil {
		return err
	}
	if leaseExpired(tk) {
		return fmt.Errorf("service: report progress on task %s: %w", taskID, model.ErrLeaseExpired)
	}
	if tk.Status == model.TaskStatusSucceeded ||
		tk.Status == model.TaskStatusFailed ||
		tk.Status == model.TaskStatusCancelled {
		return fmt.Errorf("service: report progress on task %s: status %q is terminal: %w",
			taskID, tk.Status, model.ErrInvalidState)
	}

	if tk.Status == model.TaskStatusClaimed {
		tk.Status = model.TaskStatusRunning
	}
	tk.Progress = clampProgress(progress)
	if message != "" {
		tk.Message = message
	}
	if err := s.st.UpdateTask(ctx, tk); err != nil {
		return fmt.Errorf("service: report progress on task %s: %w", taskID, err)
	}

	s.publish("task_updated", tk)
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
	tk, err := s.guardTaskMutation(ctx, agentID, taskID)
	if err != nil {
		return err
	}

	if tk.Status == model.TaskStatusSucceeded {
		return nil
	}
	if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
		return fmt.Errorf("service: submit result on task %s: status %q is not claimable: %w",
			taskID, tk.Status, model.ErrInvalidState)
	}
	if leaseExpired(tk) {
		return fmt.Errorf("service: submit result on task %s: %w", taskID, model.ErrLeaseExpired)
	}

	tk.Status = model.TaskStatusSucceeded
	tk.Progress = 1
	tk.Artifacts = artifacts
	tk.LeaseUntil = nil
	if err := s.st.UpdateTask(ctx, tk); err != nil {
		return fmt.Errorf("service: submit result on task %s: %w", taskID, err)
	}

	s.publish("task_updated", tk)
	s.audit(ctx, "agent:"+agentID, "task.submit", taskID, "")
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
	tk, err := s.guardTaskMutation(ctx, agentID, taskID)
	if err != nil {
		return err
	}

	if tk.Status == model.TaskStatusSucceeded {
		return fmt.Errorf("service: fail task %s: status %q cannot fail: %w",
			taskID, tk.Status, model.ErrInvalidState)
	}

	tk.Status = model.TaskStatusFailed
	tk.Message = reason
	tk.LeaseUntil = nil
	if err := s.st.UpdateTask(ctx, tk); err != nil {
		return fmt.Errorf("service: fail task %s: %w", taskID, err)
	}

	s.publish("task_updated", tk)
	s.audit(ctx, "agent:"+agentID, "task.fail", taskID, reason)
	return nil
}

// guardTaskMutation loads one task and applies the checks every mutating
// agent call shares: the task must exist, the caller must be its holder, and
// the check for an expired lease.
//
// AgentID ownership lives here rather than in the store because the store
// knows nothing about agents — an agent id is an identity, and identity rules
// belong to the service layer.
func (s *Service) guardTaskMutation(ctx context.Context, agentID, taskID string) (model.Task, error) {
	if agentID == "" {
		return model.Task{}, fmt.Errorf("service: mutate task %s: agent_id is required: %w", taskID, model.ErrArgument)
	}

	tk, err := s.st.GetTask(ctx, taskID)
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
func (s *Service) activeTasksForAgent(ctx context.Context, agentID string) []model.Task {
	all, err := s.st.ListActiveTasks(ctx)
	if err != nil {
		return nil
	}

	out := make([]model.Task, 0, len(all))
	for _, tk := range all {
		if tk.AgentID == agentID {
			out = append(out, tk)
		}
	}
	return out
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
