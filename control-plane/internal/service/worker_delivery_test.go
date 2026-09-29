package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func writePackage(t *testing.T, root, dir string) {
	t.Helper()
	p := filepath.Join(root, dir)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"edl.json":               "{}",
		"edit.xml":               "<xmeml/>",
		"delivery-manifest.json": `{"schema_version":1,"artifacts":[{"kind":"edl","path":"edl.json"},{"kind":"xml","path":"edit.xml"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(p, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func deliveryFixture(t *testing.T, taskType string) (*service.Service, string) {
	t.Helper()
	svc := newService(t)
	root := t.TempDir()
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "clip", Status: model.AssetStatusNarrated, AgentVisible: true,
		AllowedAgents: []string{"exporter"}, Artifacts: map[string]string{"source": "src/clip.mp4", "edl": "src/edl.json"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateTask(ctx, model.Task{TaskID: "ex", AssetID: "clip", Type: taskType, AgentRole: "exporter"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimTask(ctx, "e1", "exporter", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	return svc, root
}

func TestSubmitDeliveryRegistersPackage(t *testing.T) {
	svc, root := deliveryFixture(t, model.TaskTypeExport)
	writePackage(t, root, "clip/ex")
	ctx := context.Background()
	if err := svc.SubmitDelivery(ctx, "e1", "ex", "clip/ex"); err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.GetTask(ctx, "ex")
	a, _ := svc.GetAsset(ctx, "clip")
	if tk.Status != model.TaskStatusSucceeded || tk.Artifacts["xml"] != "clip/ex/edit.xml" {
		t.Fatalf("task: %+v", tk)
	}
	if a.Status != model.AssetStatusExported || a.Artifacts["manifest"] != "clip/ex/delivery-manifest.json" {
		t.Fatalf("asset: %+v", a)
	}
	if _, stale := a.Artifacts["source"]; stale {
		t.Fatalf("source-package key survived export: %+v", a.Artifacts)
	}
	files, err := svc.ListDeliveryFiles(ctx, "clip")
	if err != nil || len(files) != 3 {
		t.Fatalf("delivery listing after export: %v %+v", err, files)
	}
	// Retry is a no-op.
	if err := svc.SubmitDelivery(ctx, "e1", "ex", "clip/ex"); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitDeliveryRejects(t *testing.T) {
	ctx := context.Background()
	t.Run("escape", func(t *testing.T) {
		svc, _ := deliveryFixture(t, model.TaskTypeExport)
		if err := svc.SubmitDelivery(ctx, "e1", "ex", "../outside"); !errors.Is(err, model.ErrArgument) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		svc, root := deliveryFixture(t, model.TaskTypePreview)
		writePackage(t, root, "p")
		if err := svc.SubmitDelivery(ctx, "e1", "ex", "p"); !errors.Is(err, model.ErrArgument) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("no edl in package", func(t *testing.T) {
		svc, root := deliveryFixture(t, model.TaskTypeTTS)
		p := filepath.Join(root, "noedl")
		_ = os.MkdirAll(p, 0o755)
		_ = os.WriteFile(filepath.Join(p, "v.wav"), []byte("x"), 0o644)
		_ = os.WriteFile(filepath.Join(p, "delivery-manifest.json"), []byte(`{"schema_version":1,"artifacts":[{"kind":"voice","path":"v.wav"}]}`), 0o644)
		if err := svc.SubmitDelivery(ctx, "e1", "ex", "noedl"); !errors.Is(err, model.ErrArgument) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("tts package moves asset to narrated", func(t *testing.T) {
		svc, root := deliveryFixture(t, model.TaskTypeTTS)
		writePackage(t, root, "p")
		if err := svc.SubmitDelivery(ctx, "e1", "ex", "p"); err != nil {
			t.Fatal(err)
		}
		if a, _ := svc.GetAsset(ctx, "clip"); a.Status != model.AssetStatusNarrated {
			t.Fatalf("status %s", a.Status)
		}
	})
	t.Run("not holder", func(t *testing.T) {
		svc, root := deliveryFixture(t, model.TaskTypeExport)
		writePackage(t, root, "p")
		if err := svc.SubmitDelivery(ctx, "e2", "ex", "p"); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("locked after claim", func(t *testing.T) {
		svc, root := deliveryFixture(t, model.TaskTypeExport)
		writePackage(t, root, "p")
		locked := true
		if _, err := svc.PatchAssetGovernance(ctx, "tester", "clip", service.GovernancePatch{Locked: &locked}); err != nil {
			t.Fatal(err)
		}
		if err := svc.SubmitDelivery(ctx, "e1", "ex", "p"); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if tk, _ := svc.GetTask(ctx, "ex"); tk.Status == model.TaskStatusSucceeded {
			t.Fatal("locked asset still closed task")
		}
	})
}
