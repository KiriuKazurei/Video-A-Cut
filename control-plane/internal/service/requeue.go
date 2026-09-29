package service

import (
	"context"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// RequeueExpiredLeases is the recovery sweep of docs §7.2 (超时回收): every
// task whose lease has lapsed without a heartbeat or a submit is put back into
// the queue so it can be claimed again.
//
// The failure it repairs is an agent that died mid-flight. Its task stays
// claimed, owned by an identity that will never write to it again, and the
// queue never offers it to anybody else — the work is silently lost while the
// control plane reports a healthy running task. The sweep is the only thing
// that closes that gap, and running it is idempotent: a task swept twice is
// queued, holds no lease, and therefore never matches a second time.
//
// The expiry test is deliberately shared with the write path rather than
// re-implemented: the same leaseExpired helper guards ReportProgress and
// SubmitResult, so a task the sweep considers expired is exactly the task the
// stale agent can no longer write to. A sweep moment where those two
// disagree is precisely the window in which the agent overwrites the new
// holder's state.
//
// A requeued task is reset to the state ClaimTask would have produced, and
// nothing else: status back to queued, holder cleared, lease cleared, progress
// zeroed and the message dropped. The message is cleared on purpose — it is
// the dead agent's last words ("TTS 合成中 3/7"), and the next holder must not
// inherit a progress readout it did not write. ClaimedAt is left alone: it is
// the history of when the work was first picked up, which the audit trail
// already records, and a requeue is a re-queue rather than a new task.
//
// Each re-queue is announced as task_updated so the WebUI sees the task move
// back without polling, and written to the audit log under the "queue" actor:
// work being taken away from an agent and handed back is a governance-visible
// event, unlike the high-frequency progress and heartbeat writes that are
// deliberately unaudited.
//
// The caller receives how many tasks were recovered. The sweep stops at the
// first store failure and returns the count recovered so far with the error:
// the tasks already re-queued are committed, and the caller must know the
// partial progress rather than assume the sweep either fully ran or did
// nothing.
func (s *Service) RequeueExpiredLeases(ctx context.Context) (int, error) {
	tasks, err := s.st.ListActiveTasks(ctx)
	if err != nil {
		return 0, fmt.Errorf("service: requeue expired leases: %w", err)
	}

	requeued := 0
	for _, candidate := range tasks {
		var saved model.Task
		changed, exhausted := false, false
		err := s.st.Transaction(ctx, func(tx *store.Store) error {
			now := time.Now().UTC()
			tk, err := tx.GetTask(ctx, candidate.TaskID)
			if err != nil {
				return err
			}
			if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
				return nil
			}
			if tk.LeaseUntil == nil || tk.LeaseUntil.After(now) {
				return nil
			}

			tk.Attempts++
			tk.LeaseUntil = nil
			tk.Artifacts = map[string]string{}
			if tk.Attempts >= s.attemptCap() {
				// Give up: keep the last holder for the audit trail and
				// state the reason instead of handing the task out again.
				tk.Status = model.TaskStatusFailed
				tk.Message = fmt.Sprintf("lease expired %d times; retry limit reached", tk.Attempts)
				exhausted = true
			} else {
				tk.Status = model.TaskStatusQueued
				tk.AgentID = ""
				tk.Progress = 0
				tk.Message = ""
			}
			if err := tx.UpdateTask(ctx, tk); err != nil {
				return fmt.Errorf("service: requeue task %s: %w", tk.TaskID, err)
			}
			saved, err = tx.GetTask(ctx, tk.TaskID)
			changed = true
			return err
		})
		if err != nil {
			return requeued, fmt.Errorf("service: requeue task %s: %w", candidate.TaskID, err)
		}
		if !changed {
			continue
		}

		s.publish("task_updated", cloneTask(saved))
		if exhausted {
			s.audit(ctx, "queue", "task.fail", saved.TaskID, saved.Message)
			s.cascadeFailure(ctx, saved.TaskID)
			continue
		}
		s.audit(ctx, "queue", "task.requeue", saved.TaskID, fmt.Sprintf("lease expired (attempt %d)", saved.Attempts))
		requeued++
	}
	return requeued, nil
}
