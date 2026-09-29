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
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/config"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/mcpserver"
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

// archiveSweepInterval is how often the audit retention policy runs.
//
// Retention is a daily-scale operation: rows age in days, so a sweep every
// month would let a log grow far past its window and a sweep every lease
// cycle would replay the same aged rows against the database all day for no
// benefit. The value is stated here rather than left to queue's own default
// because a cadence is a deployment choice, not a rule about audits.
const archiveSweepInterval = 24 * time.Hour

// httpShutdownGrace bounds how long the HTTP surface gets to drain once the
// process is asked to stop.
//
// Shutdown has to wait for in-flight requests because a client that got a
// partial answer cannot retry it safely — a governance PATCH interrupted
// halfway through is a change the WebUI believes it made and the store never
// received. But it cannot wait forever, and the reason is specific: an SSE
// subscription is a request that never completes on its own, so a surface
// with one browser attached would otherwise hold the process open until that
// browser closed the tab. Five seconds is far longer than any request this
// plane answers (a governance write is four fields and an audit insert) and
// far shorter than the patience of a supervisor waiting for a clean exit.
const httpShutdownGrace = 5 * time.Second

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
	arch *queue.Archiver
	cfg  config.Config

	// httpSrv is the HTTP governance surface, or nil when cfg.HttpAddr is
	// empty (the documented headless deployment). It is built by assemble
	// but only served by Run, for the same reason the reclaimer is: a
	// caller that assembles and inspects must not have a listener live.
	httpSrv *http.Server
	// httpLn is the listener httpSrv serves on. It is held by the app so
	// Run does not have to bind and a test can assert the bind succeeded
	// without a request ever arriving — the bind is what a bad http_addr
	// fails, and an unbound server that reports the failure only on its
	// first request would be too late.
	httpLn net.Listener
	// httpRequests lets forced shutdown wait until active handlers have
	// observed connection closure and returned before the store is closed.
	httpRequests *requestTracker
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
	if err := svc.ConfigureDeliveryRoot(cfg.DeliveryRoot); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("control plane: delivery root: %w", err)
	}
	svc.SetBus(bus)
	svc.SetMaxAttempts(cfg.MaxAttempts)
	a := &app{
		st:   st,
		svc:  svc,
		bus:  bus,
		recl: queue.New(svc, leaseInterval(cfg)),
		arch: queue.NewArchiver(svc, archiveSweepInterval, cfg.AuditRetentionDays, cfg.ArchiveRetentionDays),
		cfg:  cfg,
	}

	// The governance surface is bound here rather than in Run so a bad
	// http_addr fails the process at startup. cfg.Validate has already
	// established the address has host:port shape; what it cannot know is
	// whether this host will actually hand it out — the port is already
	// taken, the host is not an address of this machine. Discovering that
	// after the store is open and the workers are running would mean a
	// process that looks up and has no way to report the failure except
	// through an exit code from a goroutine.
	//
	// The listener is held rather than the address, so Shutdown can close
	// it: Shutdown stops accepting new connections and waits for the
	// in-flight ones, but it does not release the underlying socket, and a
	// Close on the listener is what makes a port genuinely free again.
	if err := a.listenHTTP(); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

// listenHTTP builds the governance surface and binds it to cfg.HttpAddr.
//
// It is a no-op when the address is empty: the documented headless
// deployment runs the workers with nothing listening, and constructing a
// server for it would leave a non-nil httpSrv that serves nothing.
//
// Listen rather than ListenAndServe is the point of separating this from
// serveHTTP. ListenAndServe combines the two steps where a bind failure can
// only be reported through the call's own error, which for a serve loop
// started from Run is indistinguishable from a shutdown. Splitting them
// makes the bind a startup check with a synchronous result, and makes
// Run's serve call the thing that returns http.ErrServerClosed.
func (a *app) listenHTTP() error {
	if a.cfg.HttpAddr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", a.cfg.HttpAddr)
	if err != nil {
		// No "control plane:" prefix: startup already adds one, and a
		// doubled prefix reads as two failures to an operator.
		return fmt.Errorf("listen on %s: %w", a.cfg.HttpAddr, err)
	}
	a.httpLn = ln
	a.httpRequests = newRequestTracker()
	handler := api.New(a.svc).Handler()
	if a.cfg.WebRoot != "" {
		handler, err = sameOriginWebHandler(a.cfg.WebRoot, handler)
		if err != nil {
			return fmt.Errorf("web root: %w", err)
		}
	}
	if a.cfg.MCPAgentsFile != "" {
		mcpHandler, err := mcpserver.Load(a.cfg.MCPAgentsFile, a.svc, leaseInterval(a.cfg))
		if err != nil {
			return fmt.Errorf("mcp agents: %w", err)
		}
		mux := http.NewServeMux()
		mux.Handle("/mcp", mcpHandler)
		mux.Handle("/", handler)
		handler = mux
	}
	// Header and idle timeouts bound slow or abandoned connections. There
	// is deliberately no WriteTimeout: the SSE stream and delivery ZIP are
	// long-lived responses that manage their own cancellation.
	a.httpSrv = &http.Server{
		Handler:           a.httpRequests.wrap(handler),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	return nil
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
	// The listener is released before the bus so a surface that is still
	// accepting cannot outlive the store its handlers read from. Closing a
	// listener that Serve already closed on its own returns an error, which
	// is why it is discarded rather than collected: the second close is the
	// normal shutdown path, not a failure to report.
	if a.httpSrv != nil {
		_ = a.httpSrv.Close()
	}
	if a.httpLn != nil {
		_ = a.httpLn.Close()
	}
	if a.httpRequests != nil {
		a.httpRequests.close()
		ctx, cancel := context.WithTimeout(context.Background(), httpForceCloseGrace)
		defer cancel()
		if err := a.httpRequests.wait(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http handlers did not stop: %w", err))
		}
	}
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

// Run drives the assembled app until ctx is cancelled: it runs the lease
// reclaimer, the audit archiver and — when the configuration names an
// address — the HTTP governance surface, then blocks, so the caller only has
// to supply a cancellable context and this owns the goroutines that keep the
// process alive.
//
// Each loop reports one result to Run. If one stops unexpectedly, Run cancels
// the shared context and waits for all remaining loops to return before
// propagating the original failure.
//
// ctx.Err() is returned rather than swallowed: a caller that runs the app and
// gets nil back cannot tell "shut down cleanly" from "was asked to stop", and
// the two lead to different decisions about whether to report anything. The
// HTTP surface is the one exception in a narrower sense: serveHTTP turns a
// deliberate close into nil, so a cancelled context arrives here as
// context.Canceled rather than as a serve error dressed up as a fault. That
// conversion is what keeps an ordinary SIGTERM out of main's error branch.
//
// A serve failure the surface reports on its own is not swallowed. It would
// otherwise end in a goroutine nobody can read: the process would keep
// running with a governance surface that answers nothing, reporting healthy
// to the workers that are still sweeping.
func (a *app) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type loopResult struct {
		name string
		err  error
	}
	loopCount := 2
	if a.httpSrv != nil {
		loopCount++
	}
	results := make(chan loopResult, loopCount)
	start := func(name string, run func(context.Context) error) {
		go func() {
			err := run(runCtx)
			if err == nil && runCtx.Err() == nil {
				err = fmt.Errorf("control plane: %s loop stopped unexpectedly", name)
			}
			results <- loopResult{name: name, err: err}
		}()
	}
	start("reclaimer", a.recl.Run)
	start("archiver", a.arch.Run)
	if a.httpSrv != nil {
		start("http", a.serveHTTP)
	}

	var firstErr error
	for i := 0; i < loopCount; i++ {
		result := <-results
		if result.err != nil && (runCtx.Err() == nil || !errors.Is(result.err, runCtx.Err())) {
			if firstErr == nil {
				firstErr = fmt.Errorf("control plane: %s loop: %w", result.name, result.err)
			}
			cancel()
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

const httpForceCloseGrace = 5 * time.Second

// serveHTTP serves the governance surface until ctx is cancelled and then
// drains it. Its return value is the graceful path's failure, or nil when the
// surface closed on purpose; Run propagates it only when the caller was not
// already shutting down.
//
// Serve runs in its own goroutine and this method reports the step that
// failed rather than running Serve inline, because a drain has to be issued
// from somewhere that is not inside Serve.
//
// A deliberate close must not surface as a fault: ListenAndServe answers
// http.ErrServerClosed when the listener it was serving is closed, so the
// error is exactly what this method's own Shutdown produces, and letting it
// through would make every ordinary SIGTERM look like a serve failure in
// main's error branch and exit 1.
func (a *app) serveHTTP(ctx context.Context) error {
	// Log the bound REST/SSE address after assemble has successfully opened it,
	// so the message reports a live listener rather than a configuration value.
	log.Printf("http: listening on %s", a.cfg.HttpAddr)

	served := make(chan error, 1)
	go func() { served <- a.httpSrv.Serve(a.httpLn) }()

	// Serve runs separately so this goroutine can initiate a bounded drain when
	// the process context is cancelled, and can report an unsolicited failure
	// back to Run immediately.
	select {
	case <-ctx.Done():
	case err := <-served:
		// Serve stopped on its own. For a listener bound in assemble that
		// means the accept loop failed for a reason unrelated to shutdown,
		// so it is reported rather than mistaken for the clean path below.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.forceCloseHTTP()
			return err
		}
		return nil
	}

	// A cancelled ctx carries no deadline, and Shutdown needs one to know
	// when to stop waiting for in-flight requests. Deriving from Background
	// is therefore not an escape hatch here, it is the only correct parent:
	// the budget is a fixed wall-clock allowance that exists precisely
	// because the caller's context has already been withdrawn. Passing the
	// original ctx would make Shutdown return the moment it was called,
	// closing idle connections and nothing else, and the drain this
	// comment introduces would not happen at all.
	drainCtx, cancel := context.WithTimeout(context.Background(), httpShutdownGrace)
	defer cancel()

	err := a.httpSrv.Shutdown(drainCtx)
	// Shutdown closes listeners it has registered. Closing the held listener
	// as well covers a Shutdown that ran before Serve registered it. It is
	// harmless when shutdown already closed the socket.
	_ = a.httpLn.Close()

	if err != nil {
		// Shutdown only closes idle connections. An SSE request remains
		// active by design, so a bounded graceful drain must be followed by
		// Close to cancel every remaining request context and release its
		// bus subscription before the store is closed.
		log.Printf("http: graceful shutdown ended (%v); closing remaining connections", err)
		_ = a.httpSrv.Close()
	}
	if a.httpRequests != nil {
		a.httpRequests.close()
		forceCtx, forceCancel := context.WithTimeout(context.Background(), httpForceCloseGrace)
		waitErr := a.httpRequests.wait(forceCtx)
		forceCancel()
		if waitErr != nil {
			log.Printf("http: handlers did not stop after connection close: %v", waitErr)
		}
	}

	// Collect Serve's result before returning. A non-ErrServerClosed value is
	// an accept-loop failure; the timeout ensures a broken server shutdown
	// cannot hold Run open indefinitely.
	select {
	case serveErr := <-served:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
	case <-time.After(httpForceCloseGrace):
		return fmt.Errorf("http: serve loop did not stop after shutdown")
	}
	return nil
}

// forceCloseHTTP tears down active connections after an unexpected Serve
// failure. Run will cancel the other loops after this returns, but existing
// handlers also need their request contexts cancelled before the store closes.
func (a *app) forceCloseHTTP() {
	if a.httpSrv != nil {
		_ = a.httpSrv.Close()
	}
	if a.httpLn != nil {
		_ = a.httpLn.Close()
	}
	if a.httpRequests != nil {
		a.httpRequests.close()
		ctx, cancel := context.WithTimeout(context.Background(), httpForceCloseGrace)
		defer cancel()
		if err := a.httpRequests.wait(ctx); err != nil {
			log.Printf("http: handlers did not stop after serve failure: %v", err)
		}
	}
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

	// A termination signal is not a failure: it is how the process is
	// meant to stop, so it exits 0 without an error on stderr. Anything
	// else Run reports is a real fault and exits 1.
	if err := a.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "control plane: running: %v\n", err)
		os.Exit(1)
	}
}
