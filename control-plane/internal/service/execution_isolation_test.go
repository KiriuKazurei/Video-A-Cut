package service_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func startHeldIngest(t *testing.T, agent, instance, request string) (*ingestEnv, service.IngestStart, model.Task, *service.IngestInput, service.ExecutionScope) {
	t.Helper()
	env := newIngestEnv(t)
	ctx := context.Background()
	env.recording("held.mkv", 2048)
	reg, err := env.svc.RegisterRecording(ctx, "human:test", "asset-iso", "rec", "held.mkv", "reg-iso")
	if err != nil {
		t.Fatal(err)
	}
	env.allowIngester("asset-iso")
	start, err := env.svc.StartIngestRun(ctx, "human:test", "asset-iso", reg.Source.SourceID, reg.SourceVersion, "start-iso")
	if err != nil {
		t.Fatal(err)
	}
	env.capability(agent, ingest.RootsFingerprint(env.roots))
	tk, in, scope := env.claimOpts(agent, instance, request, true)
	return env, start, tk, in, scope
}

func taskSnapshot(t *testing.T, svc *service.Service, id string) model.Task {
	t.Helper()
	tk, err := svc.GetTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestT01StaleExecutionCannotMutateReplacement(t *testing.T) {
	env, _, tk, in, old := startHeldIngest(t, "agent-a", "inst-a", "req-a")
	ctx := context.Background()
	svc := env.svc
	past := time.Now().Add(-time.Minute)
	setTaskFixture(t, svc, tk.TaskID, func(task *model.Task) {
		task.LeaseUntil = &past
	})
	if _, err := svc.RequeueExpiredLeases(ctx); err != nil {
		t.Fatal(err)
	}
	_, current, newScope := env.claimOpts("agent-a", "inst-a", "req-a-2", false)
	if newScope.ExecutionID == old.ExecutionID || current.ExecutionID == in.ExecutionID {
		t.Fatal("replacement reused the old execution")
	}
	before := taskSnapshot(t, svc, tk.TaskID)
	ops := []struct {
		name string
		call func() error
	}{
		{"get_input", func() error { _, err := svc.GetTaskInputScoped(ctx, old); return err }},
		{"heartbeat", func() error {
			return svc.HeartbeatScoped(ctx, old.AgentID, old.Role, old.TaskID, time.Now().Add(time.Minute), old)
		}},
		{"progress", func() error { return svc.ReportProgressScoped(ctx, old, 0.9, "old execution") }},
		{"fail", func() error { return svc.FailTaskScoped(ctx, old, "old execution failed") }},
		{"checkpoint", func() error {
			_, err := svc.SaveIngestCheckpoint(ctx, old.AgentID, old.Role, service.CheckpointRequest{
				TaskID: old.TaskID, ExecutionID: old.ExecutionID, RuntimeInstanceID: old.RuntimeInstanceID,
				Generation: old.Generation, Sequence: 1, InputSHA256: old.InputSHA256, Kind: "copy_chunk",
				Ref: in.OutputDir + "/checkpoints/00001.json", SHA256: strings.Repeat("ab", 32),
			})
			return err
		}},
		{"submit", func() error {
			return svc.SubmitIngestResult(ctx, old.AgentID, old.Role, service.IngestResultRequest{
				TaskID: old.TaskID, ExecutionID: old.ExecutionID, RuntimeInstanceID: old.RuntimeInstanceID,
				Generation: old.Generation, PackageDir: in.PackageDir, InputSHA256: old.InputSHA256,
			})
		}},
	}
	for _, op := range ops {
		if err := op.call(); !errors.Is(err, model.ErrStaleExecution) {
			t.Errorf("%s err = %v, want stale_execution", op.name, err)
		}
	}
	ctrl, err := svc.GetExecutionControl(ctx, old, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ctrl.ExecutionID != old.ExecutionID || ctrl.Command != "stop" {
		t.Fatalf("old control = %+v", ctrl)
	}
	got := taskSnapshot(t, svc, tk.TaskID)
	if got.Status != before.Status || got.Progress != before.Progress || got.Message != before.Message || got.AgentID != before.AgentID {
		t.Fatalf("new task changed under stale writes: before=%+v after=%+v", before, got)
	}
	fresh, err := svc.GetTaskInputScoped(ctx, newScope)
	if err != nil || fresh.Ingest == nil || fresh.Ingest.ExecutionID != newScope.ExecutionID {
		t.Fatalf("current input = %+v %v", fresh.Ingest, err)
	}
}

func TestT02SecondInstanceDoesNotTakeOver(t *testing.T) {
	env, start, _, _, old := startHeldIngest(t, "agent-a", "inst-a", "req-a")
	ctx := context.Background()
	_, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{
		RuntimeInstanceID: "inst-b", RequestID: "req-b",
	})
	if !errors.Is(err, model.ErrInstanceConflict) {
		t.Fatalf("second instance claim err = %v", err)
	}
	still, err := env.svc.GetExecutionControl(ctx, old, 0)
	if err != nil || still.Status != model.ExecRunning || still.Command == "stop" {
		t.Fatalf("first execution changed: %+v %v", still, err)
	}
	replaced, err := env.svc.StartIngestRunWithPolicy(ctx, "human:test", "asset-iso", start.Run.SourceID, "", "replace-1", model.ResourceReplaceAfterStop)
	if err == nil {
		t.Fatal("replace without the registered source version was accepted")
	}
	_ = replaced
	reg, err := env.svc.ListRecordingSources(ctx, "asset-iso")
	if err != nil || len(reg) != 1 {
		t.Fatal(err)
	}
	out, err := env.svc.StartIngestRunWithPolicy(ctx, "agent:agent-a", "asset-iso", reg[0].SourceID, reg[0].SourceVersion, "agent-replace", model.ResourceReplaceAfterStop)
	if !errors.Is(err, model.ErrForbidden) || out.Task.TaskID != "" {
		t.Fatalf("agent replace = %+v %v", out, err)
	}
	out, err = env.svc.StartIngestRunWithPolicy(ctx, "human:test", "asset-iso", reg[0].SourceID, reg[0].SourceVersion, "human-replace", model.ResourceReplaceAfterStop)
	if err != nil || out.Disposition != model.DispositionWaitingDrain {
		t.Fatalf("replace = %+v %v", out, err)
	}
	if err := env.svc.BeginExecution(ctx, old); err == nil {
		t.Fatal("revoked execution began again")
	}
	next, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{
		RuntimeInstanceID: "inst-b", RequestID: "req-b",
	})
	if err != nil || next.Scope.ExecutionID == old.ExecutionID {
		t.Fatalf("replacement claim = %+v %v", next.Scope, err)
	}
	if err := env.svc.BeginExecution(ctx, next.Scope); err == nil {
		t.Fatal("both instances could begin before drain")
	}
	if err := env.svc.AckExecutionStopped(ctx, old, "stopped"); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.BeginExecution(ctx, old); err == nil {
		t.Fatal("old execution began after the replacement was current")
	}
	if err := env.svc.BeginExecution(ctx, next.Scope); err != nil {
		t.Fatalf("replacement begin after drain: %v", err)
	}
}

func TestT03ClaimRequestReplayDoesNotMintASecondExecution(t *testing.T) {
	env, _, tk, _, first := startHeldIngest(t, "agent-a", "inst-a", "req-lost")
	ctx := context.Background()
	again, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{
		RuntimeInstanceID: "inst-a", RequestID: "req-lost",
	})
	if err != nil || again.Obsolete || again.Scope.ExecutionID != first.ExecutionID {
		t.Fatalf("replay = %+v %v", again, err)
	}
	rows, err := storeOf(t, env.svc).ListIngestExecutionsByTask(ctx, tk.TaskID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("executions = %+v %v", rows, err)
	}
	past := time.Now().Add(-time.Minute)
	setTaskFixture(t, env.svc, tk.TaskID, func(task *model.Task) { task.LeaseUntil = &past })
	if _, err := env.svc.RequeueExpiredLeases(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{
		RuntimeInstanceID: "inst-a", RequestID: "req-new",
	})
	if err != nil || next.Scope.ExecutionID == first.ExecutionID {
		t.Fatalf("new claim = %+v %v", next.Scope, err)
	}
	replay, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{
		RuntimeInstanceID: "inst-a", RequestID: "req-lost",
	})
	if err != nil || !replay.Obsolete || replay.Scope.ExecutionID != first.ExecutionID || replay.Claimed {
		t.Fatalf("obsolete replay = %+v %v", replay, err)
	}
	if replay.Scope.ExecutionID == next.Scope.ExecutionID {
		t.Fatal("lost request was answered with the later scope")
	}
}

func TestT04ReplaceDrainThenBeginIsOrdered(t *testing.T) {
	env, _, _, _, old := startHeldIngest(t, "agent-a", "inst-a", "req-a")
	ctx := context.Background()
	cur, err := env.svc.GetExecutionControl(ctx, old, 0)
	if err != nil || cur.Command != "none" {
		t.Fatalf("control before replace = %+v %v", cur, err)
	}
	var order []string
	var mu sync.Mutex
	note := func(step string) {
		mu.Lock()
		order = append(order, step)
		mu.Unlock()
	}
	env.svc.SetControlPollHook(func() {
		note("poll_waiting")
		reg, err := env.svc.ListRecordingSources(ctx, "asset-iso")
		if err != nil {
			t.Errorf("sources: %v", err)
			return
		}
		out, err := env.svc.StartIngestRunWithPolicy(ctx, "human:test", "asset-iso", reg[0].SourceID, reg[0].SourceVersion, "replace-t04", model.ResourceReplaceAfterStop)
		if err != nil || out.Disposition != model.DispositionWaitingDrain {
			t.Errorf("replace = %+v %v", out, err)
			return
		}
		note("revoked")
	})
	started := time.Now()
	type pollResult struct {
		view service.ExecutionControl
		err  error
	}
	ch := make(chan pollResult, 1)
	go func() {
		view, err := env.svc.GetExecutionControl(ctx, old, cur.ControlVersion)
		ch <- pollResult{view, err}
	}()
	got := <-ch
	elapsed := time.Since(started)
	if got.err != nil || got.view.Command != "stop" || got.view.ExecutionID != old.ExecutionID {
		t.Fatalf("poll = %+v %v", got.view, got.err)
	}
	if elapsed > service.ControlPollMaxWait {
		t.Fatalf("stop was not observed within %s (took %s)", service.ControlPollMaxWait, elapsed)
	}
	note("stop_observed")
	next, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{RuntimeInstanceID: "inst-a", RequestID: "req-next"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.svc.BeginExecution(ctx, next.Scope); err == nil {
		t.Fatal("begin succeeded before drain")
	}
	note("begin_blocked")
	if err := env.svc.AckExecutionStopped(ctx, old, "stopped"); err != nil {
		t.Fatal(err)
	}
	note("acked")
	if err := env.svc.BeginExecution(ctx, next.Scope); err != nil {
		t.Fatalf("begin after ack: %v", err)
	}
	note("begun")
	mu.Lock()
	defer mu.Unlock()
	want := []string{"poll_waiting", "revoked", "stop_observed", "begin_blocked", "acked", "begun"}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v", order)
		}
	}
}

func TestT05RejectIfBusyCreatesNothing(t *testing.T) {
	env, start, tk, _, old := startHeldIngest(t, "agent-a", "inst-a", "req-a")
	ctx := context.Background()
	beforeTask := taskSnapshot(t, env.svc, tk.TaskID)
	beforeCtrl, err := env.svc.GetExecutionControl(ctx, old, 0)
	if err != nil {
		t.Fatal(err)
	}
	db := storeOf(t, env.svc).DB()
	var runsBefore, tasksBefore int
	if err := db.QueryRow(`SELECT count(*) FROM ingest_runs`).Scan(&runsBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM tasks`).Scan(&tasksBefore); err != nil {
		t.Fatal(err)
	}
	reg, err := env.svc.ListRecordingSources(ctx, "asset-iso")
	if err != nil {
		t.Fatal(err)
	}
	out, err := env.svc.StartIngestRunWithPolicy(ctx, "human:test", "asset-iso", reg[0].SourceID, reg[0].SourceVersion, "busy-1", model.ResourceRejectIfBusy)
	if !errors.Is(err, model.ErrResourceBusy) || out.Task.TaskID != "" || out.Run.RunID != "" {
		t.Fatalf("reject = %+v %v", out, err)
	}
	var runsAfter, tasksAfter, plans int
	_ = db.QueryRow(`SELECT count(*) FROM ingest_runs`).Scan(&runsAfter)
	_ = db.QueryRow(`SELECT count(*) FROM tasks`).Scan(&tasksAfter)
	_ = db.QueryRow(`SELECT count(*) FROM ingest_plan_revisions`).Scan(&plans)
	if runsAfter != runsBefore || tasksAfter != tasksBefore || plans != 0 {
		t.Fatalf("reject left runs %d->%d tasks %d->%d plans %d", runsBefore, runsAfter, tasksBefore, tasksAfter, plans)
	}
	got := taskSnapshot(t, env.svc, tk.TaskID)
	ctrl, err := env.svc.GetExecutionControl(ctx, old, 0)
	if err != nil || got.Status != beforeTask.Status || got.Progress != beforeTask.Progress || ctrl.Status != beforeCtrl.Status || ctrl.Command != "none" {
		t.Fatalf("old execution changed: task=%+v control=%+v %v", got, ctrl, err)
	}
	if start.Run.RunID == "" {
		t.Fatal("missing original run")
	}
}

func TestT06PersistedControlSurvivesRestartAndLeaseExpiry(t *testing.T) {
	env, _, tk, _, old := startHeldIngest(t, "agent-a", "inst-a", "req-a")
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	setTaskFixture(t, env.svc, tk.TaskID, func(task *model.Task) { task.LeaseUntil = &past })
	if _, err := env.svc.RequeueExpiredLeases(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := service.New(storeOf(t, env.svc))
	restarted.SetControlPollWait(0)
	ctrl, err := restarted.GetExecutionControl(ctx, old, 0)
	if err != nil || ctrl.Command != "stop" || ctrl.ExecutionID != old.ExecutionID {
		t.Fatalf("restarted control = %+v %v", ctrl, err)
	}
	keyOwner := ""
	barrier := 0
	if err := storeOf(t, env.svc).DB().QueryRow(`SELECT owner_execution_id, barrier FROM execution_resources`).Scan(&keyOwner, &barrier); err != nil {
		t.Fatal(err)
	}
	if keyOwner != old.ExecutionID || barrier != 1 {
		t.Fatalf("lease expiry cleared the resource owner: owner=%s barrier=%d", keyOwner, barrier)
	}
	next, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{RuntimeInstanceID: "inst-a", RequestID: "req-after-expiry"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.svc.BeginExecution(ctx, next.Scope); err == nil {
		t.Fatal("begin succeeded while the expired owner still held the resource")
	}
	if service.ControlPollMaxWait != 2*time.Second || service.ControlNetworkTimeoutMax != 3*time.Second {
		t.Fatalf("protocol waits = %s / %s", service.ControlPollMaxWait, service.ControlNetworkTimeoutMax)
	}
}

func TestT07CleanupBlockedPreventsBeginUntilOwnerMatches(t *testing.T) {
	env, _, _, in, old := startHeldIngest(t, "agent-a", "inst-a", "req-a")
	ctx := context.Background()
	keptRel := in.OutputDir + "/checkpoints/keep.json"
	neighborRel := "ingest/unrelated.txt"
	wantKept := []byte("verified")
	wantNeighbor := []byte("leave-me")
	if env.write(keptRel, wantKept) == "" || env.write(neighborRel, wantNeighbor) == "" {
		t.Fatal("checkpoint or neighbor file was not written")
	}
	assertFiles := func() {
		t.Helper()
		gotKept, err := os.ReadFile(filepath.Join(env.delivery, filepath.FromSlash(keptRel)))
		if err != nil {
			t.Fatal(err)
		}
		gotNeighbor, err := os.ReadFile(filepath.Join(env.delivery, filepath.FromSlash(neighborRel)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotKept, wantKept) || !bytes.Equal(gotNeighbor, wantNeighbor) {
			t.Fatalf("files changed: checkpoint=%q neighbor=%q", gotKept, gotNeighbor)
		}
	}
	past := time.Now().Add(-time.Minute)
	tk, err := env.svc.GetTask(ctx, old.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	setTaskFixture(t, env.svc, tk.TaskID, func(task *model.Task) { task.LeaseUntil = &past })
	if _, err := env.svc.RequeueExpiredLeases(ctx); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.AckExecutionStopped(ctx, old, "kill"); err == nil {
		t.Fatal("accepted a kill outcome")
	}
	if err := env.svc.AckExecutionStopped(ctx, old, "cleanup_blocked"); err != nil {
		t.Fatal(err)
	}
	assertFiles()
	if err := env.svc.AckExecutionStopped(ctx, old, "cleanup_blocked"); err != nil {
		t.Fatalf("repeat cleanup ack: %v", err)
	}
	assertFiles()
	next, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{RuntimeInstanceID: "inst-a", RequestID: "req-blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.svc.BeginExecution(ctx, next.Scope); err == nil {
		t.Fatal("begin succeeded while cleanup was blocked")
	}
	if err := env.svc.ResolveCleanupBlocked(ctx, "agent:agent-a", old.ExecutionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("agent resolve err = %v", err)
	}
	if err := env.svc.ResolveCleanupBlocked(ctx, "human:test", next.Scope.ExecutionID); err == nil {
		t.Fatal("unrelated execution cleared the block")
	}
	if err := env.svc.ResolveCleanupBlocked(ctx, "human:test", old.ExecutionID); err != nil {
		t.Fatal(err)
	}
	assertFiles()
	if err := env.svc.BeginExecution(ctx, next.Scope); err != nil {
		t.Fatalf("begin after owned resolve: %v", err)
	}
	assertFiles()
}

func TestT08AckDoesNotTouchTheNewTask(t *testing.T) {
	env, _, tk, _, old := startHeldIngest(t, "agent-a", "inst-a", "req-a")
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	setTaskFixture(t, env.svc, tk.TaskID, func(task *model.Task) { task.LeaseUntil = &past })
	if _, err := env.svc.RequeueExpiredLeases(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := env.svc.ClaimForExecution(ctx, "agent-a", ingester, time.Now().Add(time.Minute), service.ClaimOptions{RuntimeInstanceID: "inst-a", RequestID: "req-new"})
	if err != nil {
		t.Fatal(err)
	}
	before := taskSnapshot(t, env.svc, tk.TaskID)
	if err := env.svc.ReportProgressScoped(ctx, next.Scope, 0.2, "new execution"); err == nil {
		t.Fatal("progress was accepted before begin")
	}
	if got := taskSnapshot(t, env.svc, tk.TaskID); got.Status != before.Status || got.Progress != before.Progress || got.Message != before.Message || got.AgentID != before.AgentID {
		t.Fatalf("progress before begin changed the task: before=%+v after=%+v", before, got)
	}
	type opResult struct {
		op  string
		err error
	}
	errCh := make(chan opResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	go func() {
		ready.Done()
		ready.Wait()
		errCh <- opResult{"ack", env.svc.AckExecutionStopped(ctx, old, "stopped")}
	}()
	go func() {
		ready.Done()
		ready.Wait()
		errCh <- opResult{"progress", env.svc.ReportProgressScoped(ctx, next.Scope, 0.4, "new execution racing ack")}
	}()
	got := map[string]error{}
	for i := 0; i < 2; i++ {
		res := <-errCh
		got[res.op] = res.err
	}
	if got["ack"] != nil {
		t.Fatalf("ack: %v", got["ack"])
	}
	if got["progress"] == nil {
		t.Fatal("new execution progress was accepted before begin")
	}
	if err := env.svc.AckExecutionStopped(ctx, old, "stopped"); err != nil {
		t.Fatalf("repeat ack: %v", err)
	}
	if err := env.svc.ReportProgressScoped(ctx, old, 0.8, "old progress"); !errors.Is(err, model.ErrStaleExecution) {
		t.Fatalf("old progress err = %v, want stale_execution", err)
	}
	after := taskSnapshot(t, env.svc, tk.TaskID)
	if after.Status != before.Status || after.Progress != before.Progress || after.Message != before.Message || after.AgentID != before.AgentID {
		t.Fatalf("ack changed the new task: before=%+v after=%+v", before, after)
	}
	if err := env.svc.BeginExecution(ctx, next.Scope); err != nil {
		t.Fatalf("begin after ack: %v", err)
	}
	status, err := env.svc.GetExecutionResultStatus(ctx, old, "submit-old")
	if err != nil || status.Status != "not_committed" || status.RequestID != "submit-old" {
		t.Fatalf("old result status = %+v %v", status, err)
	}
}

func TestHistoricalTaskNeedsNoExecutionScope(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateTask(ctx, model.Task{TaskID: "tts-1", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"}); err != nil {
		t.Fatal(err)
	}
	tk, err := svc.ClaimTask(ctx, "n1", "narrator", time.Now().Add(time.Minute))
	if err != nil || tk.TaskID != "tts-1" {
		t.Fatalf("claim = %+v %v", tk, err)
	}
	if err := svc.Heartbeat(ctx, "n1", "narrator", tk.TaskID, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReportProgress(ctx, "n1", tk.TaskID, 0.4, "speaking"); err != nil {
		t.Fatal(err)
	}
	if err := svc.FailTask(ctx, "n1", tk.TaskID, "historical failure"); err != nil {
		t.Fatal(err)
	}
}
