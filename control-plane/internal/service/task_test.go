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

// seedTask creates one queued task through the service, which is the only
// path a task can come into existence by.
func seedTask(t *testing.T, svc *service.Service, id string) {
	t.Helper()
	if err := svc.CreateTask(context.Background(), model.Task{
		TaskID:    id,
		AssetID:   "clip_001",
		Type:      model.TaskTypeTTS,
		AgentRole: "narrator",
	}); err != nil {
		t.Fatalf("CreateTask(%s): %v", id, err)
	}
}

// taskQueue returns a service with one narrator-visible asset and two queued
// tasks on it, ready for a claim.
func taskQueue(t *testing.T) *service.Service {
	t.Helper()
	svc := newService(t)
	seedAsset(t, svc, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	})
	seedTask(t, svc, "t_001")
	seedTask(t, svc, "t_002")
	return svc
}

// claimOne claims a live task and fails the test on any error.
func claimOne(t *testing.T, svc *service.Service, agentID string) model.Task {
	t.Helper()
	tk, err := svc.ClaimTask(context.Background(), agentID, "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimTask(%s): %v", agentID, err)
	}
	return tk
}

// TestClaimIsIdempotentForSameAgent verifies a re-claim by the same agent
// returns the task it already holds instead of handing out a second one: a
// reconnecting agent must never accumulate a queue of its own.
func TestClaimIsIdempotentForSameAgent(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	first, err := svc.ClaimTask(ctx, "narrator-01", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("first ClaimTask: %v", err)
	}

	second, err := svc.ClaimTask(ctx, "narrator-01", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("second ClaimTask: %v", err)
	}
	if second.TaskID != first.TaskID {
		t.Fatalf("second ClaimTask returned %q, want the held task %q", second.TaskID, first.TaskID)
	}
	if second.Status != model.TaskStatusClaimed {
		t.Errorf("Status = %q, want %q", second.Status, model.TaskStatusClaimed)
	}

	// The other queued task must still be queued: the repeat claim must not
	// have consumed it.
	other, err := svc.GetTask(ctx, "t_002")
	if err != nil {
		t.Fatalf("GetTask(t_002): %v", err)
	}
	if other.Status != model.TaskStatusQueued {
		t.Errorf("t_002 status = %q, want %q", other.Status, model.TaskStatusQueued)
	}
}

// TestClaimNeverHandsHeldLeaseToAnotherAgent verifies a task under a live
// lease belongs to exactly one agent: narrator-02 must be handed the next
// queued task rather than steal narrator-01's.
func TestClaimNeverHandsHeldLeaseToAnotherAgent(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	held, err := svc.ClaimTask(ctx, "narrator-01", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimTask(narrator-01): %v", err)
	}
	if held.TaskID != "t_001" {
		t.Fatalf("first task = %q, want t_001", held.TaskID)
	}

	fresh, err := svc.ClaimTask(ctx, "narrator-02", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimTask(narrator-02): %v", err)
	}
	if fresh.TaskID != "t_002" {
		t.Fatalf("narrator-02 got %q, want t_002 (t_001 is under narrator-01's lease)", fresh.TaskID)
	}
	if fresh.AgentID != "narrator-02" {
		t.Errorf("AgentID = %q, want %q", fresh.AgentID, "narrator-02")
	}

	stored, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask(t_001): %v", err)
	}
	if stored.AgentID != "narrator-01" {
		t.Errorf("t_001 holder is %q, want narrator-01 untouched", stored.AgentID)
	}
}

// TestClaimReturnsNotFoundWhenQueueEmpty verifies an empty queue is reported
// as a missing-entity error so a poller can distinguish "try again later" from
// a real failure.
func TestClaimReturnsNotFoundWhenQueueEmpty(t *testing.T) {
	svc := newService(t)
	seedAsset(t, svc, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	})

	_, err := svc.ClaimTask(context.Background(), "narrator-01", "narrator", time.Now().Add(time.Minute))
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("ClaimTask on an empty queue: got %v, want errors.Is ErrNotFound", err)
	}
}

// TestCreateTaskForcesQueuedStatus verifies the service overrides a
// caller-supplied status: a task that could be created running would skip the
// queue and the claim path entirely.
func TestCreateTaskForcesQueuedStatus(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	seedAsset(t, svc, model.Asset{AssetID: "clip_001", Status: model.AssetStatusIngested})

	if err := svc.CreateTask(ctx, model.Task{
		TaskID:    "t_001",
		AssetID:   "clip_001",
		Type:      model.TaskTypeTTS,
		AgentRole: "narrator",
		Status:    model.TaskStatusRunning,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != model.TaskStatusQueued {
		t.Fatalf("Status = %q, want %q (a created task must start queued)", got.Status, model.TaskStatusQueued)
	}
}

// TestCreateTaskRejectsUnknownAsset verifies a task can never reference a
// dangling asset.
func TestCreateTaskRejectsUnknownAsset(t *testing.T) {
	svc := newService(t)

	err := svc.CreateTask(context.Background(), model.Task{
		TaskID:    "t_001",
		AssetID:   "no_such_asset",
		Type:      model.TaskTypeTTS,
		AgentRole: "narrator",
	})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("CreateTask on an unknown asset: got %v, want errors.Is ErrNotFound", err)
	}
}

// TestReportProgressAndSubmit drives the happy path of the task lifecycle:
// a claim, a progress report that promotes the task to running, and a result
// submission that closes it with the artifacts the agent produced.
func TestReportProgressAndSubmit(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	tk := claimOne(t, svc, "narrator-01")
	if tk.Status != model.TaskStatusClaimed {
		t.Fatalf("claimed task status = %q, want %q", tk.Status, model.TaskStatusClaimed)
	}

	const wantMessage = "TTS 合成中 3/7"
	if err := svc.ReportProgress(ctx, "narrator-01", tk.TaskID, 0.42, wantMessage); err != nil {
		t.Fatalf("ReportProgress: %v", err)
	}

	mid, err := svc.GetTask(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("GetTask after ReportProgress: %v", err)
	}
	if mid.Status != model.TaskStatusRunning {
		t.Errorf("Status = %q, want %q (the first report promotes claimed to running)", mid.Status, model.TaskStatusRunning)
	}
	if mid.Progress != 0.42 {
		t.Errorf("Progress = %v, want 0.42", mid.Progress)
	}
	if mid.Message != wantMessage {
		t.Errorf("Message = %q, want %q", mid.Message, wantMessage)
	}

	arts := map[string]string{"voice": "voice.wav"}
	if err := svc.SubmitResult(ctx, "narrator-01", tk.TaskID, arts); err != nil {
		t.Fatalf("SubmitResult: %v", err)
	}

	done, err := svc.GetTask(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("GetTask after SubmitResult: %v", err)
	}
	if done.Status != model.TaskStatusSucceeded {
		t.Fatalf("Status = %q, want %q", done.Status, model.TaskStatusSucceeded)
	}
	if done.Progress != 1 {
		t.Errorf("Progress = %v, want 1", done.Progress)
	}
	if done.Artifacts["voice"] != "voice.wav" {
		t.Errorf("Artifacts[\"voice\"] = %q, want %q", done.Artifacts["voice"], "voice.wav")
	}
	if len(done.Artifacts) != 1 {
		t.Errorf("Artifacts = %v, want exactly one entry", done.Artifacts)
	}
	if done.LeaseUntil != nil {
		t.Errorf("LeaseUntil = %v, want nil (a finished task holds no lease)", done.LeaseUntil)
	}
}

// TestSubmitResultIsIdempotent verifies a repeated submission is accepted
// without changing anything: an agent that never saw the first response
// retries, and failing that retry would report a still-running task.
func TestSubmitResultIsIdempotent(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	tk := claimOne(t, svc, "narrator-01")
	arts := map[string]string{"voice": "voice.wav"}
	if err := svc.SubmitResult(ctx, "narrator-01", tk.TaskID, arts); err != nil {
		t.Fatalf("first SubmitResult: %v", err)
	}

	if err := svc.SubmitResult(ctx, "narrator-01", tk.TaskID, arts); err != nil {
		t.Fatalf("second SubmitResult: got %v, want nil", err)
	}

	got, err := svc.GetTask(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != model.TaskStatusSucceeded {
		t.Errorf("Status = %q, want %q", got.Status, model.TaskStatusSucceeded)
	}
	if got.Artifacts["voice"] != "voice.wav" {
		t.Errorf("Artifacts[\"voice\"] = %q, want %q", got.Artifacts["voice"], "voice.wav")
	}
}

// TestStateMachineRejectsBackwards verifies a failed task cannot be reopened
// by a late heartbeat from the agent that failed it.
func TestStateMachineRejectsBackwards(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	tk := claimOne(t, svc, "narrator-01")
	if err := svc.FailTask(ctx, "narrator-01", tk.TaskID, "tts engine died"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	err := svc.ReportProgress(ctx, "narrator-01", tk.TaskID, 0.9, "still going")
	if !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("ReportProgress after FailTask: got %v, want errors.Is ErrInvalidState", err)
	}

	got, err := svc.GetTask(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != model.TaskStatusFailed {
		t.Errorf("Status = %q, want %q (the rejected report must not reopen the task)", got.Status, model.TaskStatusFailed)
	}
	if got.Message != "tts engine died" {
		t.Errorf("Message = %q, want the recorded failure reason", got.Message)
	}
}

// TestForeignAgentCannotMutate verifies only the holder may report, submit or
// fail a task: another agent failing a task it does not own would be a way to
// unqueue somebody else's work.
func TestForeignAgentCannotMutate(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	tk := claimOne(t, svc, "narrator-01")

	cases := []struct {
		name string
		err  error
	}{
		{"ReportProgress", svc.ReportProgress(ctx, "narrator-02", tk.TaskID, 0.5, "not mine")},
		{"SubmitResult", svc.SubmitResult(ctx, "narrator-02", tk.TaskID, map[string]string{"voice": "stolen.wav"})},
		{"FailTask", svc.FailTask(ctx, "narrator-02", tk.TaskID, "sabotage")},
	}
	for _, tc := range cases {
		if !errors.Is(tc.err, model.ErrForbidden) {
			t.Errorf("%s by a foreign agent: got %v, want errors.Is ErrForbidden", tc.name, tc.err)
		}
	}

	got, err := svc.GetTask(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != model.TaskStatusClaimed {
		t.Errorf("Status = %q, want %q (a foreign agent must not change state)", got.Status, model.TaskStatusClaimed)
	}
	if got.Progress != 0 {
		t.Errorf("Progress = %v, want 0", got.Progress)
	}
	if len(got.Artifacts) != 0 {
		t.Errorf("Artifacts = %v, want none", got.Artifacts)
	}
}

// TestLeaseExpiryBlocksProgressAndSubmit verifies an agent whose lease has
// lapsed cannot write to the task any more: the recovery sweep may already
// have handed it to somebody else, and a stale write would clobber the new
// holder's state.
func TestLeaseExpiryBlocksProgressAndSubmit(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	tk, err := svc.ClaimTask(ctx, "narrator-01", "narrator", time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("ClaimTask with an already-expired lease: %v", err)
	}
	if tk.Status != model.TaskStatusClaimed {
		t.Fatalf("Status = %q, want %q", tk.Status, model.TaskStatusClaimed)
	}

	err = svc.ReportProgress(ctx, "narrator-01", tk.TaskID, 0.5, "late")
	if !errors.Is(err, model.ErrLeaseExpired) {
		t.Errorf("ReportProgress with an expired lease: got %v, want errors.Is ErrLeaseExpired", err)
	}

	err = svc.SubmitResult(ctx, "narrator-01", tk.TaskID, map[string]string{"voice": "voice.wav"})
	if !errors.Is(err, model.ErrLeaseExpired) {
		t.Errorf("SubmitResult with an expired lease: got %v, want errors.Is ErrLeaseExpired", err)
	}

	got, err := svc.GetTask(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != model.TaskStatusClaimed {
		t.Errorf("Status = %q, want %q (an expired lease blocks every mutation)", got.Status, model.TaskStatusClaimed)
	}
}

// TestProgressClamped verifies a mis-scaled client cannot report more than a
// full job, nor a negative one.
func TestProgressClamped(t *testing.T) {
	svc := taskQueue(t)
	ctx := context.Background()

	seedTask(t, svc, "t_003")

	over := claimOne(t, svc, "narrator-01")
	if over.TaskID != "t_001" {
		t.Fatalf("first claim = %q, want t_001", over.TaskID)
	}
	if err := svc.ReportProgress(ctx, "narrator-01", over.TaskID, 5.0, "way over"); err != nil {
		t.Fatalf("ReportProgress(5.0): %v", err)
	}

	// narrator-01 already holds t_001 with a live lease, so claim it back from
	// a second identity — the queue is the point being exercised here, not the
	// idempotency rule.
	under, err := svc.ClaimTask(ctx, "narrator-02", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimTask(narrator-02): %v", err)
	}
	if err := svc.ReportProgress(ctx, "narrator-02", under.TaskID, -1, "negative"); err != nil {
		t.Fatalf("ReportProgress(-1): %v", err)
	}

	gotOver, err := svc.GetTask(ctx, over.TaskID)
	if err != nil {
		t.Fatalf("GetTask(over): %v", err)
	}
	if gotOver.Progress != 1 {
		t.Errorf("Progress = %v, want 1 (clamped down from 5.0)", gotOver.Progress)
	}

	gotUnder, err := svc.GetTask(ctx, under.TaskID)
	if err != nil {
		t.Fatalf("GetTask(under): %v", err)
	}
	if gotUnder.Progress != 0 {
		t.Errorf("Progress = %v, want 0 (clamped up from -1)", gotUnder.Progress)
	}
}

// TestCreateTaskRejectsEmptyFields verifies the argument guards run before
// the asset existence check, so a malformed request costs no database work.
func TestCreateTaskRejectsEmptyFields(t *testing.T) {
	svc := newService(t)
	seedAsset(t, svc, model.Asset{AssetID: "clip_001", Status: model.AssetStatusIngested})
	ctx := context.Background()

	cases := []struct {
		name string
		task model.Task
	}{
		{"empty task_id", model.Task{AssetID: "clip_001", Type: model.TaskTypeTTS, AgentRole: "narrator"}},
		{"empty asset_id", model.Task{TaskID: "t_001", Type: model.TaskTypeTTS, AgentRole: "narrator"}},
		{"empty agent_role", model.Task{TaskID: "t_001", AssetID: "clip_001", Type: model.TaskTypeTTS}},
		{"empty type", model.Task{TaskID: "t_001", AssetID: "clip_001", AgentRole: "narrator"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.CreateTask(ctx, tc.task)
			if !errors.Is(err, model.ErrArgument) {
				t.Errorf("CreateTask error = %v, want errors.Is ErrArgument", err)
			}
		})
	}
}

// TestClaimPublishesEvent verifies a claim fans one task_updated envelope
// carrying the task as the agent received it, so the SSE fan-out shows the
// claim in flight.
func TestClaimPublishesEvent(t *testing.T) {
	svc := taskQueue(t)
	bus := events.New()
	defer bus.Close()
	svc.SetBus(bus)

	got := make(chan events.Envelope, 4)
	bus.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })

	tk, err := svc.ClaimTask(context.Background(), "narrator-01", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
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
		if published.Status != model.TaskStatusClaimed {
			t.Errorf("payload status = %q, want %q", published.Status, model.TaskStatusClaimed)
		}
		if published.TaskID != tk.TaskID {
			t.Errorf("payload task id = %q, want %q", published.TaskID, tk.TaskID)
		}
		if published.AgentID != "narrator-01" {
			t.Errorf("payload agent id = %q, want %q", published.AgentID, "narrator-01")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive task_updated within 1s")
	}
}
