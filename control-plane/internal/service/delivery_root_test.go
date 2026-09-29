package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// A control plane started without delivery_root has no filesystem surface at
// all: agent delivery must fail closed rather than write somewhere default.
func TestSubmitDeliveryWithoutConfiguredRootFailsClosed(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
		AllowedAgents: []string{"exporter"}, Artifacts: map[string]string{"edl": "src/edl.json"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateTask(ctx, model.Task{TaskID: "ex", AssetID: "clip", Type: model.TaskTypeExport, AgentRole: "exporter"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimTask(ctx, "e1", "exporter", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A package created next to the store, outside any root.
	dir := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"edl.json":               "{}",
		"delivery-manifest.json": `{"schema_version":1,"artifacts":[{"kind":"edl","path":"edl.json"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := svc.SubmitDelivery(ctx, "e1", "ex", "pkg")
	if !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("SubmitDelivery without root: %v", err)
	}
	if tk, _ := svc.GetTask(ctx, "ex"); tk.Status != model.TaskStatusClaimed {
		t.Fatalf("task changed: %+v", tk)
	}
	if _, err := svc.ReadAssetEDLForRole(ctx, "exporter", "clip"); err == nil {
		t.Fatal("edl read succeeded without delivery root")
	}
}
