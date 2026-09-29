package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

var storesByService sync.Map

// newService returns a Service backed by a fresh, empty SQLite database. The
// database lives in the test's temp directory and is closed by t.Cleanup.
func newService(t *testing.T) *service.Service {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/vac.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)
	storesByService.Store(svc, st)
	return svc
}

// setTaskFixture writes intentionally unusual database states that an agent
// is not allowed to create through Service, such as an expired lease left by
// a crashed process. Tests use the store directly only for that fixture.
func setTaskFixture(t *testing.T, svc *service.Service, taskID string, edit func(*model.Task)) model.Task {
	t.Helper()
	value, ok := storesByService.Load(svc)
	if !ok {
		t.Fatalf("no store registered for service fixture")
	}
	st := value.(*store.Store)
	tk, err := st.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask fixture %s: %v", taskID, err)
	}
	edit(&tk)
	if err := st.UpdateTask(context.Background(), tk); err != nil {
		t.Fatalf("UpdateTask fixture %s: %v", taskID, err)
	}
	updated, err := st.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("read updated task fixture %s: %v", taskID, err)
	}
	return updated
}

// seedAsset creates one asset through the service and fails the test if the
// store rejects it. Tests seed via CreateAsset rather than the store so the
// visibility assertions cover the whole service surface.
func seedAsset(t *testing.T, svc *service.Service, a model.Asset) {
	t.Helper()
	if err := svc.CreateAsset(context.Background(), a); err != nil {
		t.Fatalf("CreateAsset(%s): %v", a.AssetID, err)
	}
}

// TestListVisibleAssetsFiltersByRoleAndFlags pins the single visibility
// decision point: an asset is visible to a role only when it is flagged for
// agents, is not locked, and the role is on its allowed-agents list.
func TestListVisibleAssetsFiltersByRoleAndFlags(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	seedAsset(t, svc, model.Asset{
		AssetID:       "a_visible",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	})
	seedAsset(t, svc, model.Asset{
		AssetID:       "b_hidden",
		Status:        model.AssetStatusIngested,
		AgentVisible:  false,
		AllowedAgents: []string{"narrator"},
	})
	seedAsset(t, svc, model.Asset{
		AssetID:       "c_locked",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		Locked:        true,
		AllowedAgents: []string{"narrator"},
	})
	seedAsset(t, svc, model.Asset{
		AssetID:       "d_foreign",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"exporter"},
	})

	got, err := svc.ListVisibleAssets(ctx, "narrator")
	if err != nil {
		t.Fatalf("ListVisibleAssets(narrator): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("visible assets for narrator = %d, want 1: %+v", len(got), got)
	}
	if got[0].AssetID != "a_visible" {
		t.Fatalf("visible asset = %q, want %q", got[0].AssetID, "a_visible")
	}

	// A role that matches nothing must still yield a usable, non-nil slice:
	// callers range over the result without a nil check.
	none, err := svc.ListVisibleAssets(ctx, "exporter2")
	if err != nil {
		t.Fatalf("ListVisibleAssets(exporter2): %v", err)
	}
	if none == nil {
		t.Fatal("no-match ListVisibleAssets returned a nil slice, want an empty non-nil slice")
	}
	if len(none) != 0 {
		t.Fatalf("no-match visible assets = %d, want 0: %+v", len(none), none)
	}
}

// TestListVisibleAssetsEmptyRoleReturnsNothing verifies an anonymous caller
// gets an empty slice rather than an error, so handlers can treat a missing
// role and a no-match role the same way.
func TestListVisibleAssetsEmptyRoleReturnsNothing(t *testing.T) {
	svc := newService(t)
	seedAsset(t, svc, model.Asset{
		AssetID:       "a_visible",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	})

	got, err := svc.ListVisibleAssets(context.Background(), "")
	if err != nil {
		t.Fatalf("ListVisibleAssets with empty role: %v", err)
	}
	if got == nil {
		t.Fatal("empty role returned a nil slice, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("empty role visible assets = %d, want 0: %+v", len(got), got)
	}
}

// TestApproveAssetIsIdempotent verifies approving an already-approved asset
// is accepted again without touching state, re-publishing an event or
// writing a second audit entry.
func TestApproveAssetIsIdempotent(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	bus := events.New()
	defer bus.Close()
	svc.SetBus(bus)
	if svc.Bus() != bus {
		t.Fatal("SetBus must be visible through Bus")
	}

	updates := make(chan events.Envelope, 8)
	bus.Subscribe("asset_updated", func(ev events.Envelope) { updates <- ev })

	seedAsset(t, svc, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	for i := 0; i < 2; i++ {
		if err := svc.ApproveAsset(ctx, "governor:webui", "a_1", true); err != nil {
			t.Fatalf("ApproveAsset call %d: %v", i, err)
		}
	}

	// Exactly one state change happened, so exactly one event was published.
	select {
	case ev := <-updates:
		asset, ok := ev.Payload.(model.Asset)
		if !ok {
			t.Fatalf("asset_updated payload type: got %T, want model.Asset", ev.Payload)
		}
		if !asset.HumanApproved {
			t.Fatal("published asset is not approved")
		}
	case <-time.After(time.Second):
		t.Fatal("ApproveAsset did not publish asset_updated within 1s")
	}

	select {
	case ev := <-updates:
		t.Fatalf("second ApproveAsset re-published a no-op: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// No delivery within the window: the repeat call was a no-op.
	}

	got, err := svc.GetAsset(ctx, "a_1")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if !got.HumanApproved {
		t.Error("HumanApproved = false, want true")
	}
}

// TestApproveAssetRejectsEmptyActor verifies a governance action without an
// actor is refused before any state change.
func TestApproveAssetRejectsEmptyActor(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	seedAsset(t, svc, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	err := svc.ApproveAsset(ctx, "", "a_1", true)
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("ApproveAsset with empty actor: got %v, want model.ErrArgument", err)
	}

	got, err := svc.GetAsset(ctx, "a_1")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got.HumanApproved {
		t.Error("a rejected ApproveAsset must leave HumanApproved untouched")
	}
}

// TestCreateAssetRejectsEmptyAssetID verifies the argument guard runs before
// the store is touched.
func TestCreateAssetRejectsEmptyAssetID(t *testing.T) {
	svc := newService(t)

	err := svc.CreateAsset(context.Background(), model.Asset{Status: model.AssetStatusIngested})
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("CreateAsset with empty AssetID: got %v, want model.ErrArgument", err)
	}
}

// TestCreateAssetRejectsDuplicate verifies the pre-check catches a repeated
// id as a conflict rather than letting the store error surface.
func TestCreateAssetRejectsDuplicate(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	a := model.Asset{AssetID: "a_dup", Status: model.AssetStatusIngested}
	if err := svc.CreateAsset(ctx, a); err != nil {
		t.Fatalf("first CreateAsset: %v", err)
	}

	err := svc.CreateAsset(ctx, a)
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("duplicate CreateAsset: got %v, want model.ErrConflict", err)
	}
}

// TestCreateAssetPublishesEvent verifies a successful create fans one
// asset_created envelope carrying the created asset on the attached bus.
func TestCreateAssetPublishesEvent(t *testing.T) {
	svc := newService(t)
	bus := events.New()
	defer bus.Close()
	svc.SetBus(bus)

	got := make(chan events.Envelope, 1)
	bus.Subscribe("asset_created", func(ev events.Envelope) { got <- ev })

	want := model.Asset{
		AssetID:       "a_evt",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	}
	if err := svc.CreateAsset(context.Background(), want); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	select {
	case ev := <-got:
		if ev.Name != "asset_created" {
			t.Fatalf("event name = %q, want %q", ev.Name, "asset_created")
		}
		asset, ok := ev.Payload.(model.Asset)
		if !ok {
			t.Fatalf("payload type: got %T, want model.Asset", ev.Payload)
		}
		if asset.AssetID != want.AssetID {
			t.Fatalf("payload asset id = %q, want %q", asset.AssetID, want.AssetID)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive asset_created within 1s")
	}
}

// TestGetAssetMissingReturnsNotFound verifies handlers can rely on the
// exported pass-through translating a missing row into model.ErrNotFound.
func TestGetAssetMissingReturnsNotFound(t *testing.T) {
	svc := newService(t)

	_, err := svc.GetAsset(context.Background(), "no_such_asset")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("GetAsset on a missing id: got %v, want model.ErrNotFound", err)
	}
}
