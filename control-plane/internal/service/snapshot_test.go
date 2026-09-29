package service_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestMutationReturnsAndPublishesCanonicalUnaliasedSnapshots(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	bus := events.New()
	t.Cleanup(bus.Close)
	svc.SetBus(bus)
	createdEvents := make(chan events.Envelope, 2)
	updatedEvents := make(chan events.Envelope, 2)
	bus.Subscribe("asset_created", func(ev events.Envelope) { createdEvents <- ev })
	bus.Subscribe("asset_updated", func(ev events.Envelope) { updatedEvents <- ev })

	input := model.Asset{
		AssetID:       "snapshot_asset",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator", "narrator"},
		Artifacts:     map[string]string{"edl": "before"},
	}
	if err := svc.CreateAsset(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.AllowedAgents[0] = "mutated-input"
	input.Artifacts["edl"] = "mutated-input"

	var created model.Asset
	select {
	case ev := <-createdEvents:
		var ok bool
		created, ok = ev.Payload.(model.Asset)
		if !ok {
			t.Fatalf("asset_created payload is %T", ev.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asset_created event was not delivered")
	}
	stored, err := svc.GetAsset(ctx, input.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created event omitted stored timestamps: %+v", created)
	}
	if !reflect.DeepEqual(created, stored) {
		t.Fatalf("asset_created does not match stored snapshot:\nevent: %+v\nstored: %+v", created, stored)
	}
	if !reflect.DeepEqual(created.AllowedAgents, []string{"narrator"}) || created.Artifacts["edl"] != "before" {
		t.Fatalf("caller mutation changed persisted/event data: %+v", created)
	}

	locked := true
	updated, err := svc.PatchAssetGovernance(ctx, "human:webui", input.AssetID, service.GovernancePatch{Locked: &locked})
	if err != nil {
		t.Fatal(err)
	}
	var published model.Asset
	select {
	case ev := <-updatedEvents:
		var ok bool
		published, ok = ev.Payload.(model.Asset)
		if !ok {
			t.Fatalf("asset_updated payload is %T", ev.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asset_updated event was not delivered")
	}
	stored, err = svc.GetAsset(ctx, input.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UpdatedAt.IsZero() || !reflect.DeepEqual(updated, published) || !reflect.DeepEqual(updated, stored) {
		t.Fatalf("governance response/event/store snapshots differ:\nresponse: %+v\nevent:    %+v\nstored:   %+v", updated, published, stored)
	}
}
