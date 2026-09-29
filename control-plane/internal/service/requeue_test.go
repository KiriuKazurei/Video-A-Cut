package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// claimTaskWithLease claims one task for agentID with an arbitrary lease and
// fails the test on any error. Unlike claimOne it takes the lease explicitly,
// because the sweep tests need to hand out both live and already-lapsed
// leases.
func claimTaskWithLease(t *testing.T, svc *service.Service, agentID string, lease time.Time) model.Task {
	t.Helper()
	if !lease.After(time.Now()) {
		// ClaimTask must refuse an already-dead lease. Model a process crash
		// by first making a valid claim, then seed the persisted expiry through
		// the store fixture helper.
		tk, err := svc.ClaimTask(context.Background(), agentID, "narrator", time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("ClaimTask(%s) with live lease: %v", agentID, err)
		}
		return setTaskFixture(t, svc, tk.TaskID, func(task *model.Task) { task.LeaseUntil = &lease })
	}
	tk, err := svc.ClaimTask(context.Background(), agentID, "narrator", lease)
	if err != nil {
		t.Fatalf("ClaimTask(%s): %v", agentID, err)
	}
	return tk
}

// TestRequeueExpiredLeases pins the recovery sweep of docs §7.2: a task whose
// lease has lapsed without a heartbeat or a submit goes back to the queue,
// and a task held under a live lease is left completely alone.
//
// narrator-01 claims t_001 with a lease that is already a second in the past,
// the exact state an agent that died mid-flight leaves behind, while
// narrator-02 claims t_002 with a lease a minute out. One sweep returns 1 —
// not 2, and not 0.
func TestRequeueExpiredLeases(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	claimTaskWithLease(t, svc, "narrator-01", time.Now().Add(-time.Second))
	claimTaskWithLease(t, svc, "narrator-02", time.Now().Add(time.Minute))

	got, err := svc.RequeueExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("RequeueExpiredLeases: %v", err)
	}
	if got != 1 {
		t.Fatalf("requeued = %d, want 1 (only the expired lease)", got)
	}

	swept, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if swept.Status != model.TaskStatusQueued {
		t.Errorf("t_001 status = %q, want %q (a recovered task is claimable again)", swept.Status, model.TaskStatusQueued)
	}
	if swept.AgentID != "" {
		t.Errorf("t_001 AgentID = %q, want empty (the dead holder is cleared)", swept.AgentID)
	}
	if swept.LeaseUntil != nil {
		t.Errorf("t_001 LeaseUntil = %v, want nil (no lease is left to expire)", swept.LeaseUntil)
	}
	if swept.Progress != 0 {
		t.Errorf("t_001 Progress = %v, want 0", swept.Progress)
	}
	if swept.Message != "" {
		t.Errorf("t_001 Message = %q, want empty (stale progress text is cleared)", swept.Message)
	}

	// The task under the live lease must survive untouched: a sweep that
	// reclaimed it would take work away from an agent that is still working.
	live, err := svc.GetTask(ctx, "t_002")
	if err != nil {
		t.Fatalf("GetTask(t_002): %v", err)
	}
	if live.Status != model.TaskStatusClaimed {
		t.Errorf("t_002 status = %q, want %q (a live lease is not expired)", live.Status, model.TaskStatusClaimed)
	}
	if live.AgentID != "narrator-02" {
		t.Errorf("t_002 AgentID = %q, want narrator-02", live.AgentID)
	}
	if live.LeaseUntil == nil {
		t.Error("t_002 LeaseUntil = nil, want the live lease kept")
	}
}

// TestRequeueIsNoopWhenNothingExpired verifies a queue where every lease is
// still live is a queue the sweep has nothing to do. The count is how the
// ticker decides whether to log anything at all, so a false positive here
// would be noise on every tick.
func TestRequeueIsNoopWhenNothingExpired(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	claimTaskWithLease(t, svc, "narrator-01", time.Now().Add(time.Minute))

	got, err := svc.RequeueExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("RequeueExpiredLeases: %v", err)
	}
	if got != 0 {
		t.Fatalf("requeued = %d, want 0 (no lease has lapsed)", got)
	}

	held, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if held.Status != model.TaskStatusClaimed {
		t.Errorf("t_001 status = %q, want %q (the sweep must not touch a live lease)", held.Status, model.TaskStatusClaimed)
	}
	if held.AgentID != "narrator-01" {
		t.Errorf("t_001 AgentID = %q, want narrator-01", held.AgentID)
	}
}

// TestRequeuePublishesEvent verifies a requeued task is announced as
// task_updated carrying the requeued task, so the WebUI sees the task move
// back into the queue without polling for it.
func TestRequeuePublishesEvent(t *testing.T) {
	svc := taskQueue(t)
	bus := events.New()
	defer bus.Close()
	svc.SetBus(bus)

	ctx := context.Background()
	claimTaskWithLease(t, svc, "narrator-01", time.Now().Add(-time.Second))

	// Subscribe only after the claim: ClaimTask announces itself as
	// task_updated too, and the channel is here to carry the sweep's
	// requeue, not the claim that set it up.
	got := make(chan events.Envelope, 4)
	bus.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })

	n, err := svc.RequeueExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("RequeueExpiredLeases: %v", err)
	}
	if n != 1 {
		t.Fatalf("requeued = %d, want 1", n)
	}

	select {
	case ev := <-got:
		if ev.Name != "task_updated" {
			t.Fatalf("event name = %q, want %q", ev.Name, "task_updated")
		}
		published, ok := ev.Payload.(model.Task)
		if !ok {
			t.Fatalf("payload type = %T, want model.Task", ev.Payload)
		}
		if published.Status != model.TaskStatusQueued {
			t.Errorf("payload status = %q, want %q (the event carries the requeued state)", published.Status, model.TaskStatusQueued)
		}
		if published.TaskID != "t_001" {
			t.Errorf("payload task id = %q, want t_001", published.TaskID)
		}
		if published.AgentID != "" {
			t.Errorf("payload AgentID = %q, want empty", published.AgentID)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive task_updated within 1s")
	}
}

// TestListAuditReturnsEntries verifies the audit read path surfaces the
// governance actions the write paths recorded: an approval must be readable
// afterwards with its actor, its target and a real row id, because a log you
// cannot read back cannot answer "who approved this".
func TestListAuditReturnsEntries(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	seedAsset(t, svc, model.Asset{AssetID: "clip_001", Status: model.AssetStatusIngested})
	if err := svc.ApproveAsset(ctx, "governor:webui", "clip_001", true); err != nil {
		t.Fatalf("ApproveAsset: %v", err)
	}

	got, err := svc.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("ListAudit returned no entries, want at least the approval")
	}

	var found bool
	for _, l := range got {
		if l.Action == "asset.approve" && l.Actor == "governor:webui" && l.Target == "clip_001" {
			found = true
			if l.ID <= 0 {
				t.Errorf("audit entry ID = %d, want a persisted row id > 0", l.ID)
			}
		}
	}
	if !found {
		t.Fatalf("no audit entry for asset.approve by governor:webui on clip_001: %+v", got)
	}
}

// TestRequeueCountsMultipleExpired verifies the sweep counts every expired
// task rather than stopping at the first: two agents that died leave two
// tasks stuck, and the count is what tells the operator how much work was
// recovered in one pass.
func TestRequeueCountsMultipleExpired(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	// One agent, two lapsed leases: the idempotency rule of ClaimTask only
	// short-circuits on a lease that is still live, so this exercises the
	// sweep on two separate expired holders.
	claimTaskWithLease(t, svc, "narrator-01", time.Now().Add(-time.Second))
	claimTaskWithLease(t, svc, "narrator-01", time.Now().Add(-time.Second))

	got, err := svc.RequeueExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("RequeueExpiredLeases: %v", err)
	}
	if got != 2 {
		t.Fatalf("requeued = %d, want 2 (both leases have lapsed)", got)
	}

	for _, id := range []string{"t_001", "t_002"} {
		tk, err := svc.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("GetTask(%s): %v", id, err)
		}
		if tk.Status != model.TaskStatusQueued {
			t.Errorf("%s status = %q, want %q", id, tk.Status, model.TaskStatusQueued)
		}
		if tk.LeaseUntil != nil {
			t.Errorf("%s LeaseUntil = %v, want nil", id, tk.LeaseUntil)
		}
	}
}
