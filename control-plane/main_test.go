package main

import (
	"context"
	"path/filepath"
	"testing"
)

// dbPath returns the path of a database file that exists nowhere yet. Every
// test builds its own store so no state can leak between cases.
func dbPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "vac.db")
}

// TestAssembleOpensStoreAndWiresService proves the composition root builds the
// whole object graph from a database path alone: store, service, bus and the
// reclaimer all exist afterwards, and the bus handed to the service is the one
// assemble created, not a second bus nobody publishes to.
func TestAssembleOpensStoreAndWiresService(t *testing.T) {
	a, err := assemble(dbPath(t))
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	if a.st == nil {
		t.Error("assemble: store is nil")
	}
	if a.svc == nil {
		t.Error("assemble: service is nil")
	}
	if a.bus == nil {
		t.Error("assemble: event bus is nil")
	}
	if a.recl == nil {
		t.Error("assemble: reclaimer is nil")
	}
	if a.svc.Bus() != a.bus {
		t.Error("assemble: service is not wired to the bus assemble created")
	}

	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestSelfcheckDetectsHealthyStore proves a freshly assembled app passes its
// own startup check: the probe write reaches the database and reads back.
func TestSelfcheckDetectsHealthyStore(t *testing.T) {
	a, err := assemble(dbPath(t))
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer func() { _ = a.Close() }()

	if err := a.selfcheck(context.Background()); err != nil {
		t.Errorf("selfcheck: %v", err)
	}
}

// TestSelfcheckFailsAfterStoreClosed is what stops the self-check from being
// an empty ritual. With the store closed the very first layer the check touches
// is broken, so it must report an error instead of returning nil on a process
// that cannot persist anything.
func TestSelfcheckFailsAfterStoreClosed(t *testing.T) {
	a, err := assemble(dbPath(t))
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer func() { _ = a.Close() }()

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := a.selfcheck(context.Background()); err == nil {
		t.Error("selfcheck: expected an error after the store was closed, got nil")
	}
}

// TestCloseIsSafeOnZeroApp covers the failed-startup path: assemble can return
// early on a store error, so Close must tolerate a partially built app rather
// than panicking while the real startup error is being reported.
func TestCloseIsSafeOnZeroApp(t *testing.T) {
	if err := (&app{}).Close(); err != nil {
		t.Errorf("Close on a zero app: %v", err)
	}
}

// TestSelfcheckIsIdempotent proves the check can run twice — the second start
// of a process against the same database finds the probe row already there —
// and that the probe row really is persisted afterwards.
func TestSelfcheckIsIdempotent(t *testing.T) {
	a, err := assemble(dbPath(t))
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer func() { _ = a.Close() }()

	if err := a.selfcheck(context.Background()); err != nil {
		t.Fatalf("selfcheck first run: %v", err)
	}
	if err := a.selfcheck(context.Background()); err != nil {
		t.Fatalf("selfcheck second run: %v", err)
	}

	got, err := a.svc.GetAsset(context.Background(), selfcheckAssetID)
	if err != nil {
		t.Fatalf("GetAsset(%q) after selfcheck: %v", selfcheckAssetID, err)
	}
	if got.AssetID != selfcheckAssetID {
		t.Errorf("probe asset id: got %q, want %q", got.AssetID, selfcheckAssetID)
	}
}
