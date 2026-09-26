package service

import (
	"context"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// Heartbeat is the single entry point an agent uses to say "I am alive"
// (docs §10.1). It refreshes the agent's last_seen row and, when the agent
// holds a task, renews that task's lease.
//
// Registration and renewal deliberately share one entry point. An agent that
// is still claiming its first task has no heartbeat to renew with, so it must
// be able to register itself with an empty task id; and once it is registered,
// a second call must refresh the same row rather than create a second one.
// UpsertAgent overwrites role, current_task_id and health on conflict, so a
// heartbeat both registers a stranger and renews a known agent. The row is
// written healthy because the agent is by definition alive when this call
// reaches us — staleness is what the sweep decides later, not this method.
//
// The task is then looked up by the supplied id, and three outcomes are
// possible:
//
//   - the agent is not the holder: model.ErrForbidden, because refreshing
//     somebody else's lease would steal work the sweep is supposed to give
//     back to the queue.
//   - the task is already succeeded, failed or cancelled: a silent no-op
//     returning nil. A heartbeat that races a terminal transition arrives
//     after the task is closed, and the agent would be told it failed a job
//     that is actually finished — which only makes it retry.
//   - the task is live: the lease is moved to the supplied expiry and the
//     change is published as task_updated so the WebUI sees the renewed
//     lease without polling.
//
// Unlike ReportProgress and SubmitResult, an expired lease is NOT refused
// here. This call is the very request that renews it: the agent is reporting
// in and asking for more time, so its own lapse is the thing being repaired.
// Refusing it would lock out exactly the agent the renewal is for.
//
// A heartbeat is high-frequency telemetry and is deliberately not audited,
// for the same reason ReportProgress is not: an audit row per heartbeat would
// bury the governance events that actually matter.
func (s *Service) Heartbeat(ctx context.Context, agentID, role, taskID string, lease time.Time) error {
	if agentID == "" {
		return fmt.Errorf("service: heartbeat: agent_id is required: %w", model.ErrArgument)
	}
	if role == "" {
		return fmt.Errorf("service: heartbeat: agent_role is required: %w", model.ErrArgument)
	}

	if err := s.st.UpsertAgent(ctx, model.Agent{
		AgentID:       agentID,
		Role:          role,
		Health:        model.AgentHealthHealthy,
		CurrentTaskID: taskID,
	}); err != nil {
		return fmt.Errorf("service: heartbeat for agent %s: %w", agentID, err)
	}

	// A heartbeat without a task id is a bare liveness ping: the agent is
	// idle, or is still between tasks, and there is no lease to renew.
	if taskID == "" {
		return nil
	}

	tk, err := s.st.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("service: heartbeat on task %s: %w", taskID, err)
	}
	if tk.AgentID != agentID {
		return fmt.Errorf("service: heartbeat on task %s: agent %q is not the holder: %w",
			taskID, agentID, model.ErrForbidden)
	}
	if tk.Status == model.TaskStatusSucceeded ||
		tk.Status == model.TaskStatusFailed ||
		tk.Status == model.TaskStatusCancelled {
		return nil
	}

	tk.LeaseUntil = &lease
	if err := s.st.UpdateTask(ctx, tk); err != nil {
		return fmt.Errorf("service: heartbeat on task %s: %w", taskID, err)
	}

	s.publish("task_updated", tk)
	return nil
}

// GetAgent returns one agent by id. It is a thin pass-through for the same
// reason GetTask is: transports need the raw agent row to render a worker
// view, and no rule of the service layer sits between a read and the store.
func (s *Service) GetAgent(ctx context.Context, id string) (model.Agent, error) {
	return s.st.GetAgent(ctx, id)
}
