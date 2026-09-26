// Package main is the control plane's composition root: the only place that
// knows which concrete store, bus and service instances make up a running
// process. Rules never live here — this file wires dependencies and hands
// them back.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/config"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/queue"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// defaultConfigPath is where the process reads its runtime settings when
// -config is not given. A missing file at this path is not an error — it
// means "run on defaults", which is the documented first boot.
const defaultConfigPath = "control.json"

// selfcheckAssetID is the id of the probe asset the startup check writes. The
// leading underscore keeps it out of the agent namespace, and the fixed value
// lets the check find a row a previous run of the process already left behind.
const selfcheckAssetID = "_selfcheck"

// app is the wired process. It exists so tests can assemble the same graph
// without touching process-global state such as os.Exit or signal handlers.
//
// cfg is kept rather than consumed on the way in: the surfaces planned for
// later phases need MaxAgents and the audit retention windows, and a
// composition root that threw them away would leave them unrecoverable
// except by reading the configuration file a second time.
type app struct {
	st   *store.Store
	svc  *service.Service
	bus  *events.Bus
	recl *queue.Reclaimer
	cfg  config.Config
}

// registerFlags declares the process's command-line flags on fs and returns
// pointers to the values parsed into them.
//
// It takes an explicit FlagSet rather than writing to the package-level
// flag.CommandLine so the declared defaults — the contract with an operator
// running the binary — can be asserted in a test without touching the
// process-global flag state every test in the binary shares.
func registerFlags(fs *flag.FlagSet) (configPath, dbPath *string) {
	configPath = fs.String("config", defaultConfigPath, "runtime configuration path (JSON; an absent file means defaults)")
	dbPath = fs.String("db", "vac.db", "sqlite database path")
	return configPath, dbPath
}

// leaseInterval turns the configured lease window into the reclaimer's sweep
// cadence: a lease that lasts N seconds means a stranded task cannot be
// recovered any sooner than N seconds after it died, so looking for stranded
// tasks on any other rhythm would either waste work or let a task sit stuck
// for longer than the operator asked for.
//
// The conversion is safe only because cfg.Validate guarantees LeaseSeconds
// > 0, so a Config that has not been validated must not reach this point:
// queue.New silently corrects a non-positive interval to its own 30-second
// default, which would sweep on a cadence the configuration never asked for
// with nothing anywhere reporting the substitution.
func leaseInterval(cfg config.Config) time.Duration {
	return time.Duration(cfg.LeaseSeconds) * time.Second
}

// assemble builds the whole object graph for a database file.
//
// It opens the store, attaches the event bus, and starts nothing: the
// reclaimer is constructed here but only run by Run, so a caller (including
// tests) can assemble and inspect without launching goroutines.
//
// cfg is the already-loaded configuration; assemble never reads a config file
// itself, so exactly one place understands the file format. It is validated
// here, before anything is built, because this is where the configuration
// stops being a value and starts being behaviour: the sweep cadence is
// derived from it now, and every later surface derives more.
func assemble(dbPath string, cfg config.Config) (*app, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("control plane: refusing configuration: %w", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	bus := events.New()
	svc := service.New(st)
	svc.SetBus(bus)
	return &app{
		st:   st,
		svc:  svc,
		bus:  bus,
		recl: queue.New(svc, leaseInterval(cfg)),
		cfg:  cfg,
	}, nil
}

// startup is the testable first half of main: load the configuration file,
// then assemble the process from it.
//
// Loading and assembling sit together because a failure in either has the
// same consequence — the process must not serve — and separating them would
// only give main two failure paths to report identically.
func startup(configPath, dbPath string) (*app, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading configuration from %s: %w", configPath, err)
	}
	return assemble(dbPath, cfg)
}

// Close releases every resource in reverse construction order. It is safe to
// call on a partially assembled app (nil fields are skipped), so a failed
// startup can still shut down cleanly.
func (a *app) Close() error {
	var errs []error
	if a.bus != nil {
		a.bus.Close()
	}
	if a.st != nil {
		if err := a.st.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Run drives the assembled app until ctx is cancelled: it runs the reclaimer
// and blocks, so the caller only has to supply a cancellable context and this
// owns the goroutine that keeps the process alive.
//
// The reclaimer is started through a WaitGroup rather than a bare goroutine so
// its last sweep — which may be the one that recycles a lease that lapsed
// while the process was shutting down — is not cut off by a Close on the way
// out. The bus is closed first because no user-facing subscriber exists yet;
// closing it here is what stops a future handler from being handed an event
// after the reclaimer has already stopped producing them.
func (a *app) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.recl.Run(ctx)
	}()
	wg.Wait()
	return nil
}

// selfcheck exercises one pass through every layer the process depends on,
// without a network surface. If the database cannot accept a write and read
// the value back, the process must refuse to start rather than serve requests
// that are silently failing.
//
// The probe is an asset because it is the smallest value that traverses
// service and store together: the write goes through service.CreateAsset so
// the check covers the rule layer, the store's insert and the migration
// ledger, and the read goes through GetAsset, which is the path every other
// surface uses. A read-only check would pass against a database that cannot
// accept a write, which is exactly the configuration that would fail on the
// first real request.
//
// The write is preceded by a read because CreateAsset refuses an id that
// already exists with model.ErrConflict, and a probe row left behind by a
// previous run of this process is the normal case — not a reason to fail
// startup. The probe is deliberately not deleted afterwards: removing it would
// turn the check into a transactional no-op invisible to anybody inspecting
// the store, and a stray row named "_selfcheck" is easier to explain than a
// missing one.
func (a *app) selfcheck(ctx context.Context) error {
	const writeStep = "selfcheck: write probe asset"
	const readStep = "selfcheck: read probe asset"

	probe := model.Asset{
		AssetID:       selfcheckAssetID,
		Status:        model.AssetStatusIngested,
		AgentVisible:  false,
		AllowedAgents: nil,
	}

	_, err := a.svc.GetAsset(ctx, selfcheckAssetID)
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("%s (pre-read): %w", writeStep, err)
		}
		if err := a.svc.CreateAsset(ctx, probe); err != nil {
			return fmt.Errorf("%s: %w", writeStep, err)
		}
	}

	got, err := a.svc.GetAsset(ctx, selfcheckAssetID)
	if err != nil {
		return fmt.Errorf("%s: %w", readStep, err)
	}
	if got.AssetID != selfcheckAssetID {
		return fmt.Errorf("%s: got asset %q, want %q", readStep, got.AssetID, selfcheckAssetID)
	}
	return nil
}

// main parses flags, loads the configuration, assembles the process, refuses
// to serve if either the configuration or the startup check fails, and then
// runs until a termination signal arrives.
//
// Every decision it makes is about wiring and process lifetime. The rules that
// turn bytes into tasks and tasks into assets live in service, and the loops
// that keep them running live in queue; a rule appearing here would be a
// second copy of it with a different owner.
//
// A refusal to start exits 1 for a configuration failure and for a failed
// self-check alike: to an operator both mean the same thing — the process is
// not serving — and the message on stderr is what says which.
func main() {
	configPath, dbPath := registerFlags(flag.CommandLine)
	flag.Parse()

	a, err := startup(*configPath, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "control plane: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = a.Close() }()

	if err := a.selfcheck(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "control plane: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := a.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "control plane: running: %v\n", err)
		os.Exit(1)
	}
}
