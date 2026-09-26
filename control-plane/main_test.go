package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/config"
)

// dbPath returns the path of a database file that exists nowhere yet. Every
// test builds its own store so no state can leak between cases.
func dbPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "vac.db")
}

// writeConfigFile writes doc to a config file inside a temp directory and
// returns its path, so the loading tests exercise the same JSON an operator
// edits rather than a Config built by hand.
func writeConfigFile(t *testing.T, doc string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestAssembleOpensStoreAndWiresService proves the composition root builds the
// whole object graph from a database path alone: store, service, bus and the
// reclaimer all exist afterwards, and the bus handed to the service is the one
// assemble created, not a second bus nobody publishes to.
func TestAssembleOpensStoreAndWiresService(t *testing.T) {
	a, err := assemble(dbPath(t), config.Default())
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
	a, err := assemble(dbPath(t), config.Default())
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
	a, err := assemble(dbPath(t), config.Default())
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
	a, err := assemble(dbPath(t), config.Default())
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

// TestAssembleUsesConfiguredLease proves the configured lease duration reaches
// the reclaimer instead of a constant baked into the composition root: an
// operator who raises lease_seconds must get a process that looks at stranded
// tasks on the cadence they asked for, and a process that ignored the file
// would sweep every 30s no matter what it was told.
//
// The interval is read back through the reclaimer rather than inferred,
// because that is the only place it actually matters — queue.New is what the
// sweep cadence comes from. The assertion also covers the settings that have
// no consumer yet: MaxAgents and the two retention windows must be held by
// the app, not dropped after loading, or Phase 2 would have no way to get them
// back.
func TestAssembleUsesConfiguredLease(t *testing.T) {
	cfg := config.Config{
		LeaseSeconds:         45,
		MaxAgents:            4,
		AuditRetentionDays:   7,
		ArchiveRetentionDays: 14,
	}
	a, err := assemble(dbPath(t), cfg)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer func() { _ = a.Close() }()

	if a.cfg != cfg {
		t.Errorf("assemble: app holds cfg = %+v, want %+v", a.cfg, cfg)
	}
	if got, want := a.recl.Interval(), 45*time.Second; got != want {
		t.Errorf("reclaimer interval = %v, want %v (lease_seconds drives the sweep cadence)", got, want)
	}
}

// TestAssembleDefaultsLeaseInterval pins the documented default deployment:
// with no configuration file the process sweeps every 30 seconds — the agreed
// lease window — and not some other number the composition root invented. The
// expectation is taken from config.DefaultLeaseSeconds so the two cannot
// drift apart silently.
func TestAssembleDefaultsLeaseInterval(t *testing.T) {
	a, err := assemble(dbPath(t), config.Default())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer func() { _ = a.Close() }()

	want := time.Duration(config.DefaultLeaseSeconds) * time.Second
	if got := a.recl.Interval(); got != want {
		t.Errorf("reclaimer interval = %v, want %v (the default lease window)", got, want)
	}
}

// TestAssembleRejectsUnvalidatedConfig covers the callers that never read a
// file. The reclaimer's interval is derived from LeaseSeconds, and an
// unvalidated Config carrying a zero there would hand queue.New a
// non-positive interval — which queue silently corrects to its own 30s
// default — so a configuration the process refused would end up behaving
// exactly like a valid one, with no way to tell.
func TestAssembleRejectsUnvalidatedConfig(t *testing.T) {
	cfg := config.Default()
	cfg.LeaseSeconds = 0

	a, err := assemble(dbPath(t), cfg)
	if err == nil {
		_ = a.Close()
		t.Fatal("assemble: expected an error for a Config that fails Validate, got nil")
	}
	if !errors.Is(err, config.ErrInvalid) {
		t.Errorf("assemble: error = %v, want it to wrap config.ErrInvalid", err)
	}
}

// TestStartupAppliesConfigFile proves the whole loading path end to end: the
// file an operator writes is the configuration the process assembles from,
// through to the sweep cadence the reclaimer is built with.
func TestStartupAppliesConfigFile(t *testing.T) {
	path := writeConfigFile(t, `{
		"lease_seconds": 90,
		"max_agents": 5,
		"audit_retention_days": 7,
		"archive_retention_days": 14
	}`)

	a, err := startup(path, dbPath(t))
	if err != nil {
		t.Fatalf("startup: %v", err)
	}
	defer func() { _ = a.Close() }()

	if a.cfg.LeaseSeconds != 90 {
		t.Errorf("cfg.LeaseSeconds = %d, want 90", a.cfg.LeaseSeconds)
	}
	if a.cfg.MaxAgents != 5 {
		t.Errorf("cfg.MaxAgents = %d, want 5", a.cfg.MaxAgents)
	}
	if got, want := a.recl.Interval(), 90*time.Second; got != want {
		t.Errorf("reclaimer interval = %v, want %v", got, want)
	}
}

// TestStartupRejectsInvalidConfig proves a bad configuration file stops the
// process rather than being silently ignored. main reacts to this error by
// exiting 1; what matters here is that the error escapes at all and wraps
// ErrInvalid, so the operator's log line points at the setting to fix.
func TestStartupRejectsInvalidConfig(t *testing.T) {
	path := writeConfigFile(t, `{"lease_seconds": 0}`)

	a, err := startup(path, dbPath(t))
	if err == nil {
		_ = a.Close()
		t.Fatal("startup: expected an error for lease_seconds = 0, got nil")
	}
	if !errors.Is(err, config.ErrInvalid) {
		t.Errorf("startup: error = %v, want it to wrap config.ErrInvalid", err)
	}
}

// TestDefaultConfigPath pins the documented deployment: with -config absent
// the process reads control.json, so a first boot with no file at all runs on
// defaults instead of refusing to start or looking somewhere unexpected.
//
// The flag set is built here rather than taken from the process so the
// assertion reads the declared default — the value a running process actually
// gets — instead of a constant that could have drifted away from the flag.
func TestDefaultConfigPath(t *testing.T) {
	fs := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	configPath, dbFlag := registerFlags(fs)

	if got, want := defaultConfigPath, "control.json"; got != want {
		t.Errorf("default config path = %q, want %q", got, want)
	}
	if got := fs.Lookup("config").DefValue; got != defaultConfigPath {
		t.Errorf("-config default = %q, want %q", got, defaultConfigPath)
	}
	if *configPath != defaultConfigPath {
		t.Errorf("-config value = %q, want %q", *configPath, defaultConfigPath)
	}
	if got, want := fs.Lookup("db").DefValue, "vac.db"; got != want {
		t.Errorf("-db default = %q, want %q (the database flag must survive the config change)", got, want)
	}
	if *dbFlag != "vac.db" {
		t.Errorf("-db value = %q, want %q", *dbFlag, "vac.db")
	}
}
