package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/queue"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// newStore opens a fresh SQLite database in the test's temp directory, closed
// by t.Cleanup. A second Close after a test already closed it is tolerated:
// store.Close returns nil on sql.ErrConnDone, so the failure test may drop
// the handle early without this cleanup double-closing into a panic.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/vac.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// claimable returns a Service with one narrator-visible asset and one queued
// task on it — the smallest queue that can be claimed from. Every reclaimer
// test needs the same starting state, and a task that no agent ever claims
// would prove nothing about lease recovery.
func claimable(t *testing.T) *service.Service {
	t.Helper()
	svc := service.New(newStore(t))
	ctx := context.Background()

	if err := svc.CreateAsset(ctx, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if err := svc.CreateTask(ctx, model.Task{
		TaskID:    "t_001",
		AssetID:   "clip_001",
		Type:      model.TaskTypeTTS,
		AgentRole: "narrator",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return svc
}

// claimWithLapsedLease puts t_001 in the state a crashed agent leaves behind:
// claimed, owned, and holding a lease that lapsed a second ago.
func claimWithLapsedLease(t *testing.T, svc *service.Service) {
	t.Helper()
	if _, err := svc.ClaimTask(context.Background(), "narrator-01", "narrator", time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("ClaimTask with lapsed lease: %v", err)
	}
}

// TestReclaimerReclaimsOnTick is the reason the Reclaimer exists at all: the
// ticker must actually reach the sweep, without an operator having to call
// RequeueExpiredLeases by hand.
//
// narrator-01 claims t_001 under a lease that expired a second ago — the
// exact residue of an agent that died mid-flight. The loop then runs for 150ms
// against a 20ms interval, which is several full ticks. When the context
// finally ends the loop, the task must have come back to the queue as if the
// dead agent had never held it: claimable status, no holder, no lease. And
// the run must report DeadlineExceeded rather than a sweep failure — the
// context is what stopped it, not the store.
func TestReclaimerReclaimsOnTick(t *testing.T) {
	svc := claimable(t)
	claimWithLapsedLease(t, svc)

	r := queue.New(svc, 20*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := r.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: %v, want context.DeadlineExceeded", err)
	}
	if lastErr := r.LastErr(); lastErr != nil {
		t.Errorf("LastErr = %v, want nil (a healthy sweep is not a failure)", lastErr)
	}

	got, err := svc.GetTask(context.Background(), "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if got.Status != model.TaskStatusQueued {
		t.Errorf("t_001 status = %q, want %q (a recovered task is claimable again)", got.Status, model.TaskStatusQueued)
	}
	if got.AgentID != "" {
		t.Errorf("t_001 AgentID = %q, want empty (the dead holder is cleared)", got.AgentID)
	}
	if got.LeaseUntil != nil {
		t.Errorf("t_001 LeaseUntil = %v, want nil (no lease is left to expire)", got.LeaseUntil)
	}
}

// TestReclaimerStopsOnCancel verifies the loop is a well-behaved background
// worker: it returns the moment it is told to stop, and it reports
// context.Canceled rather than some error of its own invention, so a
// supervisor can tell a deliberate shutdown apart from a crash.
//
// The cancel is issued from another goroutine after the loop has had time to
// start — a loop that only notices cancellation on the next tick would make
// shutdown wait a full interval for nothing.
func TestReclaimerStopsOnCancel(t *testing.T) {
	svc := claimable(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() { done <- queue.New(svc, 10*time.Millisecond).Run(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run: %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of cancellation")
	}
}

// TestNewDefaultsNonPositiveInterval pins the config-safety rule: a zero or
// negative interval comes from a half-written config file, and it must not
// produce either a panicking constructor or a hot loop.
//
// The assertion is indirect because `every` is unexported: the reclaimer is
// driven with such an interval for 60ms and must still exit cleanly on the
// context with no sweep failure. A degenerate interval could not do both.
func TestNewDefaultsNonPositiveInterval(t *testing.T) {
	svc := claimable(t)

	for name, every := range map[string]time.Duration{
		"zero":     0,
		"negative": -5 * time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()

			r := queue.New(svc, every)
			if err := r.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run: %v, want context.DeadlineExceeded", err)
			}
			if lastErr := r.LastErr(); lastErr != nil {
				t.Errorf("LastErr = %v, want nil", lastErr)
			}
		})
	}
}

// TestReclaimerSurvivesStoreFailure is the anti-crash rule of the ticker: the
// sweep talks to a store that can fail, and a failing store must not take the
// control plane down with it.
//
// The database is closed while the loop runs, so every sweep fails. The loop
// must still keep running until the context ends — returning early on a
// store error would silently stop recovery the moment recovery matters most —
// and the failure must be visible through LastErr, because a tick that
// quietly did nothing is indistinguishable from a healthy one until an
// operator looks.
func TestReclaimerSurvivesStoreFailure(t *testing.T) {
	st := newStore(t)
	svc := service.New(st)
	ctx := context.Background()

	// Seed through a live connection, then break it: the loop has real work
	// it cannot reach, which is precisely the failure the ticker must
	// survive.
	if err := svc.CreateAsset(ctx, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if err := svc.CreateTask(ctx, model.Task{
		TaskID:    "t_001",
		AssetID:   "clip_001",
		Type:      model.TaskTypeTTS,
		AgentRole: "narrator",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	claimWithLapsedLease(t, svc)

	if err := st.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	r := queue.New(svc, 20*time.Millisecond)
	err := r.Run(runCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: %v, want context.DeadlineExceeded (a store failure must not end the loop)", err)
	}
	// LastErr is read after Run returned, so no other goroutine is still
	// writing to it; it is guarded either way because that is the contract
	// operators rely on.
	if lastErr := r.LastErr(); lastErr == nil {
		t.Error("LastErr = nil, want the store failure recorded")
	}
}
