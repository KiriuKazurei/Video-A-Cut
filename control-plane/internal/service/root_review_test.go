package service_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// These acceptance checks deliberately use two separately opened connection
// pools. A mutex on one Service or Store cannot satisfy their invariants.
func reviewServices(t *testing.T) ([2]*service.Service, [2]*store.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review.db")
	var services [2]*service.Service
	var stores [2]*store.Store
	for i := range stores {
		st, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		stores[i], services[i] = st, service.New(st)
	}
	if err := services[0].CreateAsset(t.Context(), model.Asset{
		AssetID: "review-asset", Status: model.AssetStatusIngested,
		AgentVisible: true, AllowedAgents: []string{"narrator"},
	}); err != nil {
		t.Fatal(err)
	}
	return services, stores
}

func reviewQueueTask(t *testing.T, svc *service.Service, id string) {
	t.Helper()
	if err := svc.CreateTask(t.Context(), model.Task{
		TaskID: id, AssetID: "review-asset", Type: model.TaskTypeTTS, AgentRole: "narrator",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewIndependentStoresSerializeClaims(t *testing.T) {
	services, _ := reviewServices(t)
	reviewQueueTask(t, services[0], "one-task")
	const callers = 16
	start := make(chan struct{})
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := services[i%2].ClaimTask(t.Context(), fmt.Sprintf("agent-%d", i), "narrator", time.Now().Add(time.Minute))
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, model.ErrNotFound) {
			t.Errorf("claim error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful holders = %d, want exactly one", succeeded)
	}
}

func TestReviewIndependentStoresSameAgentIdempotency(t *testing.T) {
	services, _ := reviewServices(t)
	reviewQueueTask(t, services[0], "one-task")
	reviewQueueTask(t, services[0], "two-task")
	const callers = 12
	start := make(chan struct{})
	type result struct {
		task model.Task
		err  error
	}
	results := make(chan result, callers)
	for i := 0; i < callers; i++ {
		go func(i int) {
			<-start
			task, err := services[i%2].ClaimTask(t.Context(), "same-agent", "narrator", time.Now().Add(time.Minute))
			results <- result{task, err}
		}(i)
	}
	close(start)
	for i := 0; i < callers; i++ {
		r := <-results
		if r.err != nil || r.task.TaskID != "one-task" {
			t.Errorf("claim = %q, %v; want one-task", r.task.TaskID, r.err)
		}
	}
	other, err := services[0].GetTask(t.Context(), "two-task")
	if err != nil {
		t.Fatal(err)
	}
	if other.Status != model.TaskStatusQueued || other.AgentID != "" {
		t.Fatalf("second task was taken: %+v", other)
	}
}

func TestReviewInvalidHeartbeatLeavesAgentUntouched(t *testing.T) {
	services, stores := reviewServices(t)
	ctx := t.Context()
	if err := stores[0].UpsertAgent(ctx, model.Agent{AgentID: "review-agent", Role: "narrator", Health: model.AgentHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	before, err := services[0].GetAgent(ctx, "review-agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := services[0].Heartbeat(ctx, "review-agent", "narrator", "missing-task", time.Now().Add(time.Minute)); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("heartbeat error = %v", err)
	}
	after, err := services[1].GetAgent(ctx, "review-agent")
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("rejected heartbeat changed agent: before=%+v after=%+v", before, after)
	}
}

func TestReviewFailExpiredDoesNotChangeTask(t *testing.T) {
	services, stores := reviewServices(t)
	reviewQueueTask(t, services[0], "expired-task")
	task, err := services[0].ClaimTask(t.Context(), "owner", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Minute)
	task.LeaseUntil = &expired
	if err := stores[0].UpdateTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if err := services[1].FailTask(t.Context(), "owner", task.TaskID, "late failure"); !errors.Is(err, model.ErrLeaseExpired) {
		t.Fatalf("late failure error = %v", err)
	}
	after, err := services[0].GetTask(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != model.TaskStatusClaimed || after.Message != task.Message {
		t.Fatalf("late failure changed task: %+v", after)
	}
}

func TestReviewTransactionPanicReleasesWriteLock(t *testing.T) {
	_, stores := reviewServices(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected callback panic")
			}
		}()
		_ = stores[0].Transaction(t.Context(), func(tx *store.Store) error {
			if err := tx.CreateAsset(t.Context(), model.Asset{AssetID: "rolled-back", Status: model.AssetStatusIngested}); err != nil {
				t.Fatal(err)
			}
			panic("review rollback")
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := stores[1].CreateAsset(ctx, model.Asset{AssetID: "after-panic", Status: model.AssetStatusIngested}); err != nil {
		t.Fatalf("write lock leaked after panic: %v", err)
	}
	if _, err := stores[1].GetAsset(ctx, "rolled-back"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("panicking transaction was not rolled back: %v", err)
	}
}

func TestReviewIndependentStoresMergeGovernancePatch(t *testing.T) {
	services, _ := reviewServices(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	value := true
	go func() {
		<-start
		_, err := services[0].PatchAssetGovernance(t.Context(), "reviewer", "review-asset", service.GovernancePatch{Locked: &value})
		results <- err
	}()
	go func() {
		<-start
		_, err := services[1].PatchAssetGovernance(t.Context(), "reviewer", "review-asset", service.GovernancePatch{HumanApproved: &value})
		results <- err
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	got, err := services[0].GetAsset(t.Context(), "review-asset")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Locked || !got.HumanApproved || !got.AgentVisible {
		t.Fatalf("partial updates lost fields: %+v", got)
	}
}

func TestReviewProgressCannotReopenConcurrentCompletion(t *testing.T) {
	services, _ := reviewServices(t)
	reviewQueueTask(t, services[0], "completing-task")
	task, err := services[0].ClaimTask(t.Context(), "owner", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results <- services[i%2].ReportProgress(t.Context(), "owner", task.TaskID, 0.5, "halfway")
		}(i)
	}
	close(start)
	if err := services[1].SubmitResult(t.Context(), "owner", task.TaskID, map[string]string{"voice": "voice.wav"}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, model.ErrInvalidState) {
			t.Errorf("progress returned unexpected error: %v", err)
		}
	}
	got, err := services[0].GetTask(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.TaskStatusSucceeded || got.Progress != 1 || got.Artifacts["voice"] != "voice.wav" || got.LeaseUntil != nil {
		t.Fatalf("completed task was overwritten: %+v", got)
	}
}

func TestReviewLateHeartbeatCannotAcquireSecondLiveTask(t *testing.T) {
	services, stores := reviewServices(t)
	reviewQueueTask(t, services[0], "first-task")
	reviewQueueTask(t, services[0], "second-task")
	first, err := services[0].ClaimTask(t.Context(), "owner", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Minute)
	first.LeaseUntil = &expired
	if err := stores[0].UpdateTask(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	second, err := services[1].ClaimTask(t.Context(), "owner", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.TaskID != "second-task" {
		t.Fatalf("second claim: %+v", second)
	}
	err = services[0].Heartbeat(t.Context(), "owner", "narrator", first.TaskID, time.Now().Add(time.Minute))
	if !errors.Is(err, model.ErrLeaseHeld) && !errors.Is(err, model.ErrConflict) {
		t.Fatalf("late heartbeat must refuse a second live task, got %v", err)
	}
	got, err := services[1].GetTask(t.Context(), first.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.Equal(expired) {
		t.Fatalf("late heartbeat revived an abandoned task: %+v", got)
	}
}
