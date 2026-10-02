package service_test

import (
	"context"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"testing"
)

func TestCommittedExecutionResultRemainsReadableByOriginalOwner(t *testing.T) {
	env, _, tk, in, scope := startHeldIngest(t, "recovery-a", "recovery-inst", "recovery-claim")
	env.probePackage(tk, in, 2048)
	req := service.IngestResultRequest{TaskID: tk.TaskID, ExecutionID: scope.ExecutionID, RuntimeInstanceID: scope.RuntimeInstanceID,
		Generation: scope.Generation, PackageDir: in.PackageDir, InputSHA256: in.InputSHA256, RequestID: "committed-request"}
	ctx := context.Background()
	if err := env.svc.SubmitIngestResult(ctx, scope.AgentID, scope.Role, req); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.GetExecutionResultStatus(ctx, scope, req.RequestID)
	if err != nil || got.Status != "committed" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if err := env.svc.SubmitIngestResult(ctx, scope.AgentID, scope.Role, req); err != nil {
		t.Fatal(err)
	}
	other := scope
	other.RuntimeInstanceID = "another-instance"
	if _, err := env.svc.GetExecutionResultStatus(ctx, other, req.RequestID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("foreign result query=%v", err)
	}
	if err := env.svc.ReportProgressScoped(ctx, scope, 0.4, "late"); err == nil {
		t.Fatal("successful history regained write permission")
	}
	if len(in.ResourceKeys) != 1 {
		t.Fatalf("missing physical resource key: %+v", in)
	}
}

func TestBeginReplayKeepsCurrentResourceAndRejectsForeignRuntime(t *testing.T) {
	env, _, _, _, scope := startHeldIngest(t, "begin-replay", "begin-instance", "begin-request")
	ctx := context.Background()
	if err := env.svc.BeginExecution(ctx, scope); err != nil {
		t.Fatal(err)
	}
	foreign := scope
	foreign.RuntimeInstanceID = "foreign-runtime"
	if err := env.svc.BeginExecution(ctx, foreign); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("foreign begin=%v", err)
	}
	control, err := env.svc.GetExecutionControl(ctx, scope, 0)
	if err != nil || control.Status != model.ExecRunning {
		t.Fatalf("control=%+v err=%v", control, err)
	}
}
