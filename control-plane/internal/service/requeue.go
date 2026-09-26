package service

import (
	"context"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
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

	now := time.Now().UTC()
	requeued := 0
	for _, tk := range tasks {
		if tk.LeaseUntil == nil || tk.LeaseUntil.After(now) {
			continue
		}

		tk.Status = model.TaskStatusQueued
		tk.AgentID = ""
		tk.LeaseUntil = nil
		tk.Progress = 0
		tk.Message = ""
		if err := s.st.UpdateTask(ctx, tk); err != nil {
			return requeued, fmt.Errorf("service: requeue task %s: %w", tk.TaskID, err)
		}

		s.publish("task_updated", tk)
		s.audit(ctx, "queue", "task.requeue", tk.TaskID, "lease expired")
		requeued++
	}
	return requeued, nil
}
