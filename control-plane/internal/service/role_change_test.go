package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// An operator re-assigns an agent id to another role in the credentials
// file and restarts. The stale agents row must not lock the agent out.
func TestHeartbeatAfterRoleReassignment(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if err := svc.Heartbeat(ctx, "a1", "narrator", "", lease()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Heartbeat(ctx, "a1", "mixer", "", lease()); err != nil {
		t.Fatalf("heartbeat after role change: %v", err)
	}
	ag, err := svc.GetAgent(ctx, "a1")
	if err != nil || ag.Role != "mixer" {
		t.Fatalf("agent row = %+v %v", ag, err)
	}
}

func TestRoleChangeBlockedWhileHoldingLease(t *testing.T) {
	svc := orchFixture(t)
	ctx := context.Background()
	mustCreate(t, svc, model.Task{TaskID: "tts", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"})
	if _, err := svc.ClaimTask(ctx, "a1", "narrator", lease()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Heartbeat(ctx, "a1", "exporter", "", lease()); !errors.Is(err, model.ErrLeaseHeld) {
		t.Fatalf("role switch with live lease: %v", err)
	}
	// Claiming under the new role is refused too (existing ClaimTask rule).
	if _, err := svc.ClaimTask(ctx, "a1", "exporter", lease()); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("claim under new role with live lease: %v", err)
	}
	if err := svc.SubmitResult(ctx, "a1", "tts", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Heartbeat(ctx, "a1", "exporter", "", lease()); err != nil {
		t.Fatalf("role switch after lease ended: %v", err)
	}
}
