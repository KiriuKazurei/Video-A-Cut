package service_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// TestAuditPublishNotificationOnSuccess verifies that when an operation triggers
// an audit log, audit.changed is published after the audit record is safely written to store,
// and the newly written record is immediately readable via ListAudit.
func TestAuditPublishNotificationOnSuccess(t *testing.T) {
	svc := newService(t)
	bus := events.New()
	t.Cleanup(bus.Close)
	svc.SetBus(bus)

	ctx := context.Background()
	seedAsset(t, svc, model.Asset{
		AssetID:      "asset_audit_test",
		Status:       model.AssetStatusIngested,
		AgentVisible: false,
	})

	var mu sync.Mutex
	var receivedEnvelopes []events.Envelope
	delivered := make(chan struct{}, 1)

	subID := bus.Subscribe("audit.changed", func(ev events.Envelope) {
		mu.Lock()
		receivedEnvelopes = append(receivedEnvelopes, ev)
		mu.Unlock()
		select {
		case delivered <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() { bus.UnsubscribeID(subID) })

	// Update asset governance, which calls s.audit(ctx, actor, "asset.governance", assetID, detail)
	_, err := svc.UpdateAssetGovernance(ctx, "human:operator", "asset_audit_test", model.Asset{
		AssetID:       "asset_audit_test",
		AgentVisible:  true,
		Locked:        false,
		HumanApproved: true,
	})
	if err != nil {
		t.Fatalf("UpdateAssetGovernance: %v", err)
	}

	select {
	case <-delivered:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for audit.changed event on bus")
	}

	mu.Lock()
	count := len(receivedEnvelopes)
	var lastEnv events.Envelope
	if count > 0 {
		lastEnv = receivedEnvelopes[count-1]
	}
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected at least one audit.changed event, got 0")
	}
	if lastEnv.Name != "audit.changed" {
		t.Fatalf("event name = %q, want audit.changed", lastEnv.Name)
	}

	// Payload should be an empty object
	if payloadMap, ok := lastEnv.Payload.(map[string]any); ok {
		if len(payloadMap) != 0 {
			t.Fatalf("expected empty payload map, got %+v", payloadMap)
		}
	} else if _, ok := lastEnv.Payload.(struct{}); !ok {
		t.Fatalf("expected payload to be empty map or struct{}, got %T", lastEnv.Payload)
	}

	// Verify the audit row is already readable in store
	logs, err := svc.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	var found bool
	for _, l := range logs {
		if l.Action == "asset.governance" && l.Target == "asset_audit_test" {
			found = true
			if !strings.Contains(l.Detail, "agent_visible=true") {
				t.Fatalf("audit detail missing expected update: %q", l.Detail)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected audit log entry not found in ListAudit: %+v", logs)
	}
}

// TestAuditNoNotificationOnStoreFailure verifies that if WriteAudit fails,
// no audit.changed notification is emitted.
func TestAuditNoNotificationOnStoreFailure(t *testing.T) {
	svc := newService(t)
	bus := events.New()
	t.Cleanup(bus.Close)
	svc.SetBus(bus)

	// Close the underlying store so any subsequent WriteAudit will fail
	val, ok := storesByService.Load(svc)
	if !ok {
		t.Fatal("no store found for service")
	}
	st := val.(interface{ Close() error })
	if err := st.Close(); err != nil {
		t.Fatalf("closing store: %v", err)
	}

	delivered := make(chan struct{}, 1)
	subID := bus.Subscribe("audit.changed", func(ev events.Envelope) {
		select {
		case delivered <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() { bus.UnsubscribeID(subID) })

	// An action that calls s.audit; since store is closed, WriteAudit fails and logs error
	ctx := context.Background()
	_, _ = svc.UpdateAssetGovernance(ctx, "human:operator", "non_existent", model.Asset{
		AssetID: "non_existent",
	})

	select {
	case <-delivered:
		t.Fatal("audit.changed should NOT be published when WriteAudit fails")
	case <-time.After(150 * time.Millisecond):
		// Expected: no notification emitted
	}
}
