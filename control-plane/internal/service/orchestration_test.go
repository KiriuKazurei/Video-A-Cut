package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func orchFixture(t *testing.T) *service.Service {
	t.Helper()
	svc := newService(t)
	ctx := context.Background()
	for _, a := range []model.Asset{
		{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
			AllowedAgents: []string{"narrator", "exporter"}, Artifacts: map[string]string{"edl": "src/edl.json"}},
		{AssetID: "other", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}},
	} {
		if err := svc.CreateAsset(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	return svc
}

func mustCreate(t *testing.T, svc *service.Service, tk model.Task) {
	t.Helper()
	if err := svc.CreateTask(context.Background(), tk); err != nil {
		t.Fatalf("create %s: %v", tk.TaskID, err)
	}
}

func lease() time.Time { return time.Now().Add(time.Minute) }

func TestDependencyGatesClaim(t *testing.T) {
	svc := orchFixture(t)
	ctx := context.Background()
	mustCreate(t, svc, model.Task{TaskID: "tts", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"})
	mustCreate(t, svc, model.Task{TaskID: "exp", AssetID: "clip", Type: model.TaskTypeExport, AgentRole: "exporter", DependsOn: []string{"tts", "tts"}})
	if tk, _ := svc.GetTask(ctx, "exp"); len(tk.DependsOn) != 1 {
		t.Fatalf("dedupe: %+v", tk.DependsOn)
	}
	if _, err := svc.ClaimTask(ctx, "e1", "exporter", lease()); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("export claimed before dependency: %v", err)
	}
	if _, err := svc.ClaimTask(ctx, "n1", "narrator", lease()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimTask(ctx, "e1", "exporter", lease()); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("export claimed while dependency running: %v", err)
	}
	if err := svc.SubmitResult(ctx, "n1", "tts", map[string]string{"voice": "v.wav"}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ClaimTask(ctx, "e1", "exporter", lease())
	if err != nil || got.TaskID != "exp" {
		t.Fatalf("export after dependency: %+v %v", got, err)
	}
}

func TestDependencyValidation(t *testing.T) {
	svc := orchFixture(t)
	mustCreate(t, svc, model.Task{TaskID: "a", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"})
	mustCreate(t, svc, model.Task{TaskID: "o", AssetID: "other", Type: model.TaskTypeTTS, AgentRole: "narrator"})
	for name, tc := range map[string]struct {
		tk   model.Task
		want error
	}{
		"missing": {model.Task{TaskID: "x1", AssetID: "clip", Type: "tts", AgentRole: "narrator", DependsOn: []string{"nope"}}, model.ErrNotFound},
		"self":    {model.Task{TaskID: "x2", AssetID: "clip", Type: "tts", AgentRole: "narrator", DependsOn: []string{"x2"}}, model.ErrArgument},
		"cross":   {model.Task{TaskID: "x3", AssetID: "clip", Type: "tts", AgentRole: "narrator", DependsOn: []string{"o"}}, model.ErrArgument},
		"bad id":  {model.Task{TaskID: "x4", AssetID: "clip", Type: "tts", AgentRole: "narrator", DependsOn: []string{"a/b"}}, model.ErrArgument},
	} {
		if err := svc.CreateTask(context.Background(), tc.tk); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v want %v", name, err, tc.want)
		}
	}
}

func TestFailureCascadesToQueuedDependents(t *testing.T) {
	svc := orchFixture(t)
	ctx := context.Background()
	mustCreate(t, svc, model.Task{TaskID: "tts", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"})
	mustCreate(t, svc, model.Task{TaskID: "mix", AssetID: "clip", Type: model.TaskTypeMix, AgentRole: "narrator", DependsOn: []string{"tts"}})
	mustCreate(t, svc, model.Task{TaskID: "exp", AssetID: "clip", Type: model.TaskTypeExport, AgentRole: "exporter", DependsOn: []string{"mix"}})
	mustCreate(t, svc, model.Task{TaskID: "free", AssetID: "clip", Type: model.TaskTypeSubtitle, AgentRole: "narrator"})
	if _, err := svc.ClaimTask(ctx, "n1", "narrator", lease()); err != nil {
		t.Fatal(err)
	}
	if err := svc.FailTask(ctx, "n1", "tts", "voice model offline"); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"mix": model.TaskStatusFailed, "exp": model.TaskStatusFailed, "free": model.TaskStatusQueued} {
		tk, _ := svc.GetTask(ctx, id)
		if tk.Status != want {
			t.Errorf("%s = %s want %s (%s)", id, tk.Status, want, tk.Message)
		}
	}
	if tk, _ := svc.GetTask(ctx, "exp"); tk.Message != "dependency mix did not succeed" {
		t.Errorf("exp message: %q", tk.Message)
	}
	logs, _ := svc.ListAudit(ctx, 100)
	cascaded := 0
	for _, l := range logs {
		if l.Actor == "queue" && l.Action == "task.fail" {
			cascaded++
		}
	}
	if cascaded != 2 {
		t.Errorf("cascade audit rows = %d", cascaded)
	}
	// A new task cannot depend on a failed one.
	if err := svc.CreateTask(ctx, model.Task{TaskID: "late", AssetID: "clip", Type: "mix", AgentRole: "narrator", DependsOn: []string{"tts"}}); !errors.Is(err, model.ErrInvalidState) {
		t.Errorf("depend on failed: %v", err)
	}
}

func TestExportHeldForExportedAsset(t *testing.T) {
	// deliveryFixture claims "ex" for e1 on an asset with an edl.
	svc, root := deliveryFixture(t, model.TaskTypeExport)
	ctx := context.Background()
	writePackage(t, root, "clip/ex")
	if err := svc.SubmitDelivery(ctx, "e1", "ex", "clip/ex"); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, svc, model.Task{TaskID: "ex2", AssetID: "clip", Type: model.TaskTypeExport, AgentRole: "exporter"})
	if _, err := svc.ClaimTask(ctx, "e1", "exporter", lease()); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("export on exported asset was claimable: %v", err)
	}
	if tk, _ := svc.GetTask(ctx, "ex2"); tk.Status != model.TaskStatusQueued {
		t.Fatalf("held export should stay queued: %+v", tk)
	}
}

func TestRetryLimitFailsTask(t *testing.T) {
	svc := orchFixture(t)
	svc.SetMaxAttempts(2)
	ctx := context.Background()
	mustCreate(t, svc, model.Task{TaskID: "tts", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"})
	mustCreate(t, svc, model.Task{TaskID: "mix", AssetID: "clip", Type: model.TaskTypeMix, AgentRole: "narrator", DependsOn: []string{"tts"}})
	short := func() time.Time { return time.Now().Add(40 * time.Millisecond) }
	for round := 1; round <= 2; round++ {
		if tk, err := svc.ClaimTask(ctx, "n1", "narrator", short()); err != nil || tk.TaskID != "tts" {
			t.Fatalf("round %d claim: %+v %v", round, tk, err)
		}
		time.Sleep(80 * time.Millisecond)
		if _, err := svc.RequeueExpiredLeases(ctx); err != nil {
			t.Fatal(err)
		}
	}
	tk, _ := svc.GetTask(ctx, "tts")
	if tk.Status != model.TaskStatusFailed || tk.Attempts != 2 || tk.AgentID != "n1" {
		t.Fatalf("after cap: %+v", tk)
	}
	if m, _ := svc.GetTask(ctx, "mix"); m.Status != model.TaskStatusFailed {
		t.Fatalf("dependent not cascaded: %+v", m)
	}
	// Late submit from the dead holder cannot resurrect it.
	if err := svc.SubmitResult(ctx, "n1", "tts", nil); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("late submit: %v", err)
	}
}

func TestAttemptsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	open := func() (*service.Service, func()) {
		st, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return service.New(st), func() { _ = st.Close() }
	}
	ctx := context.Background()
	svc, closeFn := open()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}}); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, svc, model.Task{TaskID: "tts", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"})
	if _, err := svc.ClaimTask(ctx, "n1", "narrator", time.Now().Add(40*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	closeFn() // process dies holding the lease
	time.Sleep(80 * time.Millisecond)
	svc, closeFn = open()
	defer closeFn()
	if n, err := svc.RequeueExpiredLeases(ctx); err != nil || n != 1 {
		t.Fatalf("startup sweep: %d %v", n, err)
	}
	tk, _ := svc.GetTask(ctx, "tts")
	if tk.Status != model.TaskStatusQueued || tk.Attempts != 1 || tk.AgentID != "" {
		t.Fatalf("after restart: %+v", tk)
	}
}
