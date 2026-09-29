package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestReopenAssetStartsNextRound(t *testing.T) {
	svc, root := deliveryFixture(t, model.TaskTypeExport)
	ctx := context.Background()
	if _, err := svc.ReopenAsset(ctx, "human:webui", "clip"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("reopen before export: %v", err)
	}
	writePackage(t, root, "clip/ex")
	if err := svc.SubmitDelivery(ctx, "e1", "ex", "clip/ex"); err != nil {
		t.Fatal(err)
	}
	a, err := svc.ReopenAsset(ctx, "human:webui", "clip")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != model.AssetStatusIngested || a.Artifacts["edl"] != "clip/ex/edl.json" || !a.AgentVisible {
		t.Fatalf("reopened: %+v", a)
	}
	mustCreate(t, svc, model.Task{TaskID: "ex2", AssetID: "clip", Type: model.TaskTypeExport, AgentRole: "exporter"})
	if tk, err := svc.ClaimTask(ctx, "e1", "exporter", lease()); err != nil || tk.TaskID != "ex2" {
		t.Fatalf("second round claim: %+v %v", tk, err)
	}
	logs, _ := svc.ListAudit(ctx, 50)
	found := false
	for _, l := range logs {
		found = found || (l.Action == "asset.reopen" && l.Target == "clip" && l.Actor == "human:webui")
	}
	if !found {
		t.Fatal("asset.reopen not audited")
	}
	if _, err := svc.ReopenAsset(ctx, "", "clip"); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("empty actor: %v", err)
	}
}
