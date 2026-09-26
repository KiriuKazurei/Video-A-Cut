package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// countGovernanceAudits returns how many audit rows the human governance
// update entry point wrote. Tests use it to prove the idempotent path really
// is silent: a repeated identical update must not grow the log.
func countGovernanceAudits(t *testing.T, svc *service.Service) int {
	t.Helper()
	logs, err := svc.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	n := 0
	for _, l := range logs {
		if l.Action == "asset.governance" {
			n++
		}
	}
	return n
}

// TestListAllAssetsIncludesHidden pins the human governance view: it returns
// every asset the operator is responsible for, including the ones the agent
// visibility rules hide. A hidden, a locked and an empty-allow-list asset are
// invisible to every agent through ListVisibleAssets, yet an operator must
// still see them — a governance screen that cannot show the asset it is about
// to lock or grant is useless.
func TestListAllAssetsIncludesHidden(t *testing.T) {
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

	got, err := svc.ListAllAssets(ctx)
	if err != nil {
		t.Fatalf("ListAllAssets: %v", err)
	}
	if got == nil {
		t.Fatal("ListAllAssets returned a nil slice, want an empty non-nil slice on a populated store")
	}
	if len(got) != 3 {
		t.Fatalf("ListAllAssets = %d assets, want 3 (hidden and locked assets belong in the governance view): %+v", len(got), got)
	}
}

// TestListAllAssetsEmptyIsNonNil guards the contract callers rely on: an
// empty governance view is an empty slice, not nil, so a handler can render
// and range over it without a nil check.
func TestListAllAssetsEmptyIsNonNil(t *testing.T) {
	svc := newService(t)

	got, err := svc.ListAllAssets(context.Background())
	if err != nil {
		t.Fatalf("ListAllAssets on an empty store: %v", err)
	}
	if got == nil {
		t.Fatal("ListAllAssets on an empty store returned nil, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("ListAllAssets on an empty store = %d assets, want 0: %+v", len(got), got)
	}
}

// TestUpdateAssetGovernanceRejectsEmptyActor verifies a governance update
// without an attributable human is refused. An unattributable change to
// visibility, lock or approval would make the audit log unable to answer
// "who did this", which is the whole point of auditing governance.
func TestUpdateAssetGovernanceRejectsEmptyActor(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	seedAsset(t, svc, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	_, err := svc.UpdateAssetGovernance(ctx, "", "a_1", model.Asset{AgentVisible: false})
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("UpdateAssetGovernance with empty actor: got %v, want model.ErrArgument", err)
	}

	got, err := svc.GetAsset(ctx, "a_1")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got.AgentVisible {
		t.Error("a rejected governance update must leave the asset untouched")
	}
}

// TestUpdateAssetGovernanceUnknownAsset verifies a missing row is reported as
// model.ErrNotFound so a handler can answer 404 instead of silently creating
// or clobbering state.
func TestUpdateAssetGovernanceUnknownAsset(t *testing.T) {
	svc := newService(t)

	_, err := svc.UpdateAssetGovernance(context.Background(), "human:operator", "no_such_asset", model.Asset{AgentVisible: false})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("UpdateAssetGovernance on a missing asset: got %v, want model.ErrNotFound", err)
	}
}

// TestUpdateAssetGovernanceIgnoresNonGovernanceFields is the write-scope
// guard: the caller hands over a whole model.Asset, but only the governance
// fields are honoured. A tampered id, status or artifacts map in the same
// object must not reach the row, because the governance entry point has no
// business moving a pipeline along or rewriting the ingest payload.
func TestUpdateAssetGovernanceIgnoresNonGovernanceFields(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	seedAsset(t, svc, model.Asset{
		AssetID:       "a_1",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	})
	seedAsset(t, svc, model.Asset{
		AssetID: "other_1",
		Status:  model.AssetStatusRecognized,
	})

	_, err := svc.UpdateAssetGovernance(ctx, "human:operator", "a_1", model.Asset{
		AssetID:       "other_1",
		Status:        "hacked",
		AgentVisible:  true,
		Locked:        true,
		HumanApproved: true,
		AllowedAgents: []string{"narrator", "exporter"},
		Artifacts:     map[string]string{"edl.json": "../../etc/passwd"},
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("UpdateAssetGovernance: %v", err)
	}

	got, err := svc.GetAsset(ctx, "a_1")
	if err != nil {
		t.Fatalf("GetAsset(a_1): %v", err)
	}
	if got.AssetID != "a_1" {
		t.Errorf("AssetID = %q, want %q (the governance update may not re-target a row)", got.AssetID, "a_1")
	}
	if got.Status != model.AssetStatusIngested {
		t.Errorf("Status = %q, want %q (status is pipeline state, not governance state)", got.Status, model.AssetStatusIngested)
	}
	if len(got.Artifacts) != 0 {
		t.Errorf("Artifacts = %v, want empty (artifacts belong to the ingest path)", got.Artifacts)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt = zero, want the stored creation time (a governance update must not rewrite history)")
	}
	if !got.Locked || !got.HumanApproved {
		t.Errorf("Locked = %v, HumanApproved = %v, want both true (the governance fields must still be applied)", got.Locked, got.HumanApproved)
	}

	other, err := svc.GetAsset(ctx, "other_1")
	if err != nil {
		t.Fatalf("GetAsset(other_1): %v", err)
	}
	if other.Status != model.AssetStatusRecognized {
		t.Errorf("other_1 status = %q, want %q (the update must not land on a different row)", other.Status, model.AssetStatusRecognized)
	}
}

// TestUpdateAssetGovernanceFlipsFlags verifies the governance entry point
// actually moves the three flags and hands back the stored, updated asset so
// a caller never has to re-read to render what it just wrote.
func TestUpdateAssetGovernanceFlipsFlags(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	seedAsset(t, svc, model.Asset{
		AssetID:       "a_1",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	})

	got, err := svc.UpdateAssetGovernance(ctx, "human:operator", "a_1", model.Asset{
		AssetID:       "a_1",
		AgentVisible:  false,
		Locked:        true,
		HumanApproved: true,
	})
	if err != nil {
		t.Fatalf("UpdateAssetGovernance: %v", err)
	}
	if got.AgentVisible {
		t.Error("returned asset AgentVisible = true, want false (the returned value must be the updated one)")
	}
	if !got.Locked || !got.HumanApproved {
		t.Errorf("returned asset Locked = %v, HumanApproved = %v, want both true", got.Locked, got.HumanApproved)
	}
	if got.Status != model.AssetStatusIngested {
		t.Errorf("returned asset Status = %q, want the stored %q", got.Status, model.AssetStatusIngested)
	}

	stored, err := svc.GetAsset(ctx, "a_1")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if stored.AgentVisible || !stored.Locked || !stored.HumanApproved {
		t.Errorf("stored asset = %+v, want AgentVisible=false Locked=true HumanApproved=true", stored)
	}
}

// TestUpdateAssetGovernanceIsIdempotent verifies repeating an update that
// asks for the state already in place is a no-op: no second audit row, no
// second event. Without this, one double click on the governance screen
// writes two indistinguishable "who changed this" rows and pushes a
// duplicate asset_updated to every SSE subscriber.
func TestUpdateAssetGovernanceIsIdempotent(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	bus := events.New()
	defer bus.Close()
	svc.SetBus(bus)

	seedAsset(t, svc, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	updates := make(chan events.Envelope, 8)
	bus.Subscribe("asset_updated", func(ev events.Envelope) { updates <- ev })

	upd := model.Asset{AssetID: "a_1", AgentVisible: false, Locked: true}
	first, err := svc.UpdateAssetGovernance(ctx, "human:operator", "a_1", upd)
	if err != nil {
		t.Fatalf("first UpdateAssetGovernance: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("UpdateAssetGovernance did not publish asset_updated within 1s")
	}

	second, err := svc.UpdateAssetGovernance(ctx, "human:operator", "a_1", upd)
	if err != nil {
		t.Fatalf("second UpdateAssetGovernance: %v", err)
	}

	select {
	case ev := <-updates:
		t.Fatalf("an identical repeated update re-published asset_updated: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// No delivery inside the window: the repeat was a no-op.
	}

	if n := countGovernanceAudits(t, svc); n != 1 {
		t.Fatalf("asset.governance audit rows = %d, want 1 (the repeated no-op must not be audited)", n)
	}
	if first.AssetID != second.AssetID || first.Locked != second.Locked || first.AgentVisible != second.AgentVisible {
		t.Error("the repeated call returned a different asset than the first, want the same state")
	}
}

// TestUpdateAssetGovernanceAuditsChanges verifies the audit trail answers
// "who did what to whom": the human actor, the governance action, the target
// and a detail line that names the fields that actually moved. A detail that
// cannot distinguish a lock from an un-hide forces an operator to diff the
// whole row by hand.
func TestUpdateAssetGovernanceAuditsChanges(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	seedAsset(t, svc, model.Asset{
		AssetID:       "a_1",
		Status:        model.AssetStatusIngested,
		AgentVisible:  false,
		AllowedAgents: []string{"narrator"},
	})

	_, err := svc.UpdateAssetGovernance(ctx, "human:operator", "a_1", model.Asset{
		AssetID:       "a_1",
		AgentVisible:  true,
		Locked:        false,
		HumanApproved: false,
		AllowedAgents: []string{"narrator", "cutter"},
	})
	if err != nil {
		t.Fatalf("UpdateAssetGovernance: %v", err)
	}

	logs, err := svc.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	var got *model.AuditLog
	for i := range logs {
		if logs[i].Action == "asset.governance" && logs[i].Target == "a_1" {
			got = &logs[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no asset.governance audit row for a_1: %+v", logs)
	}
	if got.Actor != "human:operator" {
		t.Errorf("audit actor = %q, want %q (a human governance action must be attributable to its human)", got.Actor, "human:operator")
	}
	if got.Detail == "" {
		t.Fatal("audit detail is empty, want the changed governance fields listed")
	}
	if !strings.Contains(got.Detail, "agent_visible=true") {
		t.Errorf("audit detail = %q, want it to name agent_visible=true", got.Detail)
	}
	if !strings.Contains(got.Detail, "allowed_agents=+cutter") {
		t.Errorf("audit detail = %q, want it to name the added allowed_agents entry", got.Detail)
	}
	if strings.Contains(got.Detail, "locked=locked") {
		t.Errorf("audit detail = %s contains a malformed locked entry", got.Detail)
	}
	if strings.Contains(got.Detail, "status") {
		t.Errorf("audit detail = %q, want no non-governance field in it", got.Detail)
	}
}

// TestUpdateAssetGovernanceNormalizesAllowedAgents verifies the allowed
// agents list is stored deduplicated and sorted, so the audit detail and the
// wire payload are deterministic: two rows that differ only by order or a
// repeated entry must not look like two different states.
func TestUpdateAssetGovernanceNormalizesAllowedAgents(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	seedAsset(t, svc, model.Asset{
		AssetID:       "a_1",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	})

	got, err := svc.UpdateAssetGovernance(ctx, "human:operator", "a_1", model.Asset{
		AssetID:       "a_1",
		AgentVisible:  true,
		AllowedAgents: []string{"b", "a", "b"},
	})
	if err != nil {
		t.Fatalf("UpdateAssetGovernance: %v", err)
	}
	want := []string{"a", "b"}
	if len(got.AllowedAgents) != len(want) {
		t.Fatalf("returned AllowedAgents = %v, want %v", got.AllowedAgents, want)
	}
	for i := range want {
		if got.AllowedAgents[i] != want[i] {
			t.Fatalf("returned AllowedAgents = %v, want %v (sorted, deduplicated)", got.AllowedAgents, want)
		}
	}

	stored, err := svc.GetAsset(ctx, "a_1")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if len(stored.AllowedAgents) != 2 || stored.AllowedAgents[0] != "a" || stored.AllowedAgents[1] != "b" {
		t.Fatalf("stored AllowedAgents = %v, want [a b]", stored.AllowedAgents)
	}
}
