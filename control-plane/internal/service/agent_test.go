package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// claimedTask returns a service with one narrator-visible asset, two queued
// tasks, and t_001 held by agentID under a one-minute lease. It is the
// starting position every heartbeat test needs: a live, owned task.
func claimedTask(t *testing.T, agentID string) (*service.Service, context.Context) {
	t.Helper()
	svc := taskQueue(t)
	ctx := context.Background()

	tk, err := svc.ClaimTask(ctx, agentID, "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimTask(%s): %v", agentID, err)
	}
	if tk.TaskID != "t_001" {
		t.Fatalf("claimed task = %q, want t_001", tk.TaskID)
	}
	return svc, ctx
}

// TestHeartbeatRefreshesLease verifies the point of the call: an agent that
// keeps reporting in keeps its task, and the lease it was given at claim time
// moves out to the expiry the heartbeat supplies. The renewal must land on the
// stored task, not just in the response.
func TestHeartbeatRefreshesLease(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")
	want := time.Now().Add(2 * time.Minute)

	if err := svc.Heartbeat(ctx, "narrator-01", "narrator", "t_001", want); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	got, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if got.LeaseUntil == nil {
		t.Fatal("LeaseUntil = nil after a heartbeat, want the renewed lease")
	}
	if !got.LeaseUntil.Equal(want) {
		t.Errorf("LeaseUntil = %s, want %s", got.LeaseUntil, want)
	}
	if got.Status != model.TaskStatusClaimed {
		t.Errorf("Status = %q, want %q (a heartbeat must not change task state)", got.Status, model.TaskStatusClaimed)
	}
	if got.AgentID != "narrator-01" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "narrator-01")
	}
}

// TestHeartbeatRegistersAgent verifies the registration half of the shared
// entry point: the heartbeat is also the first thing an agent ever sends, so
// it must leave a readable agent row carrying the identity the agent declared
// and the task it is working on.
func TestHeartbeatRegistersAgent(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")

	if err := svc.Heartbeat(ctx, "narrator-01", "narrator", "t_001", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	got, err := svc.GetAgent(ctx, "narrator-01")
	if err != nil {
		t.Fatalf("GetAgent(narrator-01): %v", err)
	}
	if got.AgentID != "narrator-01" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "narrator-01")
	}
	if got.Role != "narrator" {
		t.Errorf("Role = %q, want %q", got.Role, "narrator")
	}
	if got.CurrentTaskID != "t_001" {
		t.Errorf("CurrentTaskID = %q, want %q", got.CurrentTaskID, "t_001")
	}
	if got.Health != model.AgentHealthHealthy {
		t.Errorf("Health = %q, want %q", got.Health, model.AgentHealthHealthy)
	}
	if got.LastSeen.IsZero() {
		t.Error("LastSeen is zero, want the heartbeat timestamp")
	}
}

// TestHeartbeatWithoutTaskRegistersAgentOnly verifies a bare liveness ping:
// an idle agent, or one still claiming its first task, must be registered
// without any lease work being attempted and without an error for having
// named no task.
func TestHeartbeatWithoutTaskRegistersAgentOnly(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")

	if err := svc.Heartbeat(ctx, "narrator-01", "narrator", "", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Heartbeat with no task: %v", err)
	}

	got, err := svc.GetAgent(ctx, "narrator-01")
	if err != nil {
		t.Fatalf("GetAgent(narrator-01): %v", err)
	}
	if got.CurrentTaskID != "" {
		t.Errorf("CurrentTaskID = %q, want \"\" (the heartbeat named no task)", got.CurrentTaskID)
	}
	if got.Health != model.AgentHealthHealthy {
		t.Errorf("Health = %q, want %q", got.Health, model.AgentHealthHealthy)
	}

	// The task the agent holds keeps the lease the claim gave it: a bare
	// heartbeat must not reach into a task it did not name.
	held, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if held.LeaseUntil == nil {
		t.Fatal("LeaseUntil = nil, want the lease the claim issued")
	}
}

// TestHeartbeatForForeignTaskIsRejected verifies a lease can only be renewed
// by its holder: extending a task the agent does not own would steal work the
// recovery sweep is supposed to hand back to the queue.
func TestHeartbeatForForeignTaskIsRejected(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")

	before, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}

	err = svc.Heartbeat(ctx, "narrator-02", "narrator", "t_001", time.Now().Add(2*time.Minute))
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("Heartbeat by a foreign agent: got %v, want errors.Is ErrForbidden", err)
	}

	after, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if after.AgentID != "narrator-01" {
		t.Errorf("AgentID = %q, want narrator-01 untouched", after.AgentID)
	}
	if after.LeaseUntil == nil || !after.LeaseUntil.Equal(*before.LeaseUntil) {
		t.Errorf("LeaseUntil = %v, want the rejected heartbeat to leave it at %v", after.LeaseUntil, before.LeaseUntil)
	}
}

// TestHeartbeatOnTerminalTaskIsNoop verifies a heartbeat that races a
// terminal transition is accepted silently: the task is closed, so there is no
// lease left to renew, and failing the call would report a failure on work
// that is actually finished.
func TestHeartbeatOnTerminalTaskIsNoop(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")

	if err := svc.FailTask(ctx, "narrator-01", "t_001", "tts engine died"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	err := svc.Heartbeat(ctx, "narrator-01", "narrator", "t_001", time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Heartbeat on a terminal task: got %v, want nil", err)
	}

	got, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if got.Status != model.TaskStatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, model.TaskStatusFailed)
	}
	if got.LeaseUntil != nil {
		t.Errorf("LeaseUntil = %v, want nil (a terminal task holds no lease)", got.LeaseUntil)
	}
	if got.Message != "tts engine died" {
		t.Errorf("Message = %q, want the recorded failure reason untouched", got.Message)
	}
}

// TestHeartbeatRejectsEmptyFields verifies the identity guards run before any
// store call: an agent id is what makes the row addressable and a role is what
// makes the worker meaningful, so a heartbeat missing either is refused with
// model.ErrArgument rather than half-applied.
func TestHeartbeatRejectsEmptyFields(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")

	cases := []struct {
		name    string
		agentID string
		role    string
	}{
		{"empty agent_id", "", "narrator"},
		{"empty role", "narrator-01", ""},
		{"empty both", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.Heartbeat(ctx, tc.agentID, tc.role, "t_001", time.Now().Add(time.Minute))
			if !errors.Is(err, model.ErrArgument) {
				t.Errorf("Heartbeat error = %v, want errors.Is ErrArgument", err)
			}
		})
	}

	// The rejected calls must not have registered anything or touched the
	// task the real agent holds.
	if _, err := svc.GetAgent(ctx, "narrator-01"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetAgent after rejected heartbeats: got %v, want errors.Is ErrNotFound", err)
	}
	got, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if got.AgentID != "narrator-01" {
		t.Errorf("AgentID = %q, want narrator-01 untouched", got.AgentID)
	}
}

// TestHeartbeatPublishesRenewedLease verifies the fan-out half of a renewal:
// the WebUI must see the new lease without polling, so a successful heartbeat
// on a live task publishes one task_updated envelope carrying the renewed
// task.
func TestHeartbeatPublishesRenewedLease(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")
	bus := events.New()
	defer bus.Close()
	svc.SetBus(bus)

	got := make(chan events.Envelope, 4)
	bus.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })

	want := time.Now().Add(2 * time.Minute)
	if err := svc.Heartbeat(ctx, "narrator-01", "narrator", "t_001", want); err != nil {
		t.Fatalf("Heartbeat: %v", err)
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
		if published.TaskID != "t_001" {
			t.Errorf("payload task id = %q, want %q", published.TaskID, "t_001")
		}
		if published.LeaseUntil == nil || !published.LeaseUntil.Equal(want) {
			t.Errorf("payload LeaseUntil = %v, want %s", published.LeaseUntil, want)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive task_updated within 1s")
	}
}

// TestHeartbeatOnTerminalTaskPublishesNothing verifies a no-op heartbeat
// stays a no-op on the wire: a closed task that fans out task_updated would
// make the WebUI show a lease being renewed on a job that is over.
func TestHeartbeatOnTerminalTaskPublishesNothing(t *testing.T) {
	svc, ctx := claimedTask(t, "narrator-01")
	bus := events.New()
	defer bus.Close()
	svc.SetBus(bus)

	got := make(chan events.Envelope, 4)
	bus.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })

	if err := svc.FailTask(ctx, "narrator-01", "t_001", "tts engine died"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	// Drain the failure's own task_updated so only the heartbeat under test
	// can still arrive.
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive the FailTask task_updated within 1s")
	}

	if err := svc.Heartbeat(ctx, "narrator-01", "narrator", "t_001", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Heartbeat on a terminal task: %v", err)
	}

	select {
	case ev := <-got:
		t.Fatalf("heartbeat on a terminal task published %q, want nothing", ev.Name)
	case <-time.After(200 * time.Millisecond):
		// No delivery within the window: the no-op stayed silent.
	}
}
