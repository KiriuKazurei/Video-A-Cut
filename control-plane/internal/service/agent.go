package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
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

	var saved model.Task
	changed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		currentTask := ""
		var tk model.Task
		if taskID != "" {
			var err error
			tk, err = tx.GetTask(ctx, taskID)
			if err != nil {
				return fmt.Errorf("service: heartbeat on task %s: %w", taskID, err)
			}
			if tk.AgentID != agentID {
				return fmt.Errorf("service: heartbeat on task %s: agent %q is not the holder: %w",
					taskID, agentID, model.ErrForbidden)
			}
			if tk.AgentRole != role {
				return fmt.Errorf("service: heartbeat on task %s: role %q does not match task role %q: %w",
					taskID, role, tk.AgentRole, model.ErrForbidden)
			}
			active, err := activeTasksForAgent(ctx, tx, agentID)
			if err != nil {
				return fmt.Errorf("service: heartbeat for agent %s: %w", agentID, err)
			}
			var otherLiveTask string
			now := time.Now().UTC()
			for _, held := range active {
				if held.TaskID == taskID ||
					(held.Status != model.TaskStatusClaimed && held.Status != model.TaskStatusRunning) ||
					held.LeaseUntil == nil || !held.LeaseUntil.After(now) {
					continue
				}
				otherLiveTask = held.TaskID
				break
			}
			switch tk.Status {
			case model.TaskStatusSucceeded, model.TaskStatusFailed, model.TaskStatusCancelled:
				// A terminal task is no longer the agent's current work,
				// but do not erase another live lease's pointer.
				currentTask = otherLiveTask
			case model.TaskStatusClaimed, model.TaskStatusRunning:
				if otherLiveTask != "" {
					return fmt.Errorf("service: heartbeat on task %s: agent already holds live task %s: %w",
						taskID, otherLiveTask, model.ErrLeaseHeld)
				}
				if !validLeaseUntil(lease, time.Now().UTC()) {
					return fmt.Errorf("service: heartbeat on task %s: lease expiry must be in the future: %w",
						taskID, model.ErrArgument)
				}
				tk.LeaseUntil = &lease
				if err := tx.UpdateTask(ctx, tk); err != nil {
					return fmt.Errorf("service: heartbeat on task %s: %w", taskID, err)
				}
				saved, err = tx.GetTask(ctx, taskID)
				if err != nil {
					return err
				}
				changed = true
				currentTask = taskID
			default:
				return fmt.Errorf("service: heartbeat on task %s: status %q is not active: %w",
					taskID, tk.Status, model.ErrInvalidState)
			}
		}

		agent, err := tx.GetAgent(ctx, agentID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("service: heartbeat for agent %s: %w", agentID, err)
		}
		// The authenticated role comes from the operator's credentials file
		// and wins over the stored row: a reassigned agent must not be locked
		// out by its own history. The one exception is a live lease under
		// the old role — switching then would let the agent keep working a
		// task it is no longer entitled to — so the change waits until that
		// lease ends (submit, fail, or recovery).
		if err != nil || agent.Role != role {
			held, err := activeTasksForAgent(ctx, tx, agentID)
			if err != nil {
				return fmt.Errorf("service: heartbeat for agent %s: %w", agentID, err)
			}
			now := time.Now().UTC()
			for _, tk := range held {
				if (tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning) &&
					tk.LeaseUntil != nil && tk.LeaseUntil.After(now) && tk.AgentRole != role {
					return fmt.Errorf("service: heartbeat for agent %s: role changed to %q while holding %s task %s: %w",
						agentID, role, tk.AgentRole, tk.TaskID, model.ErrLeaseHeld)
				}
			}
		}
		if err := tx.UpsertAgent(ctx, model.Agent{
			AgentID:       agentID,
			Role:          role,
			Health:        model.AgentHealthHealthy,
			CurrentTaskID: currentTask,
		}); err != nil {
			return fmt.Errorf("service: heartbeat for agent %s: %w", agentID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.publish("task_updated", cloneTask(saved))
	}
	return nil
}

// GetAgent returns one agent by id. It is a thin pass-through for the same
// reason GetTask is: transports need the raw agent row to render a worker
// view, and no rule of the service layer sits between a read and the store.
func (s *Service) GetAgent(ctx context.Context, id string) (model.Agent, error) {
	return s.st.GetAgent(ctx, id)
}
