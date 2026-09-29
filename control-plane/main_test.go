package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func headlessConfig() config.Config {
	cfg := config.Default()
	cfg.HttpAddr = ""
	return cfg
}

// TestAssembleBuildsAuditArchiver proves the composition root also builds
// the audit retention driver. An archiver that exists but is never wired
// into Run would be a silent feature: the retention policy the operator
// configured would never run, and nothing would report that it was not.
func TestAssembleBuildsAuditArchiver(t *testing.T) {
	a, err := assemble(dbPath(t), headlessConfig())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if a.arch == nil {
		t.Fatal("assemble: audit archiver is nil")
	}
}

// TestRunDrivesBothPeriodicLoops proves Run starts the archiver as well as
// the reclaimer. The archiver's own package proves it sweeps correctly when
// run; what this test pins is the wiring — that a running control plane
// actually has two periodic loops in flight rather than only the lease one,
// which is the failure mode a signature change in Run would cause.
func TestRunDrivesBothPeriodicLoops(t *testing.T) {
	a, err := assemble(dbPath(t), headlessConfig())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// The archiver sweeps immediately on start, so give it a moment to
	// reach its first pass before cancelling. Without a running archiver
	// the Run goroutine would still be alive (the reclaimer holds it), so
	// the cancellation below is what proves Run is not stuck.
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of cancellation")
	}
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
	a, err := assemble(dbPath(t), headlessConfig())
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
	a, err := assemble(dbPath(t), headlessConfig())
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
	a, err := assemble(dbPath(t), headlessConfig())
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
	a, err := assemble(dbPath(t), headlessConfig())
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
	a, err := assemble(dbPath(t), headlessConfig())
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
		"archive_retention_days": 14,
		"http_addr": ""
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

// serveHTTPStartupGrace bounds how long a test waits for the HTTP surface to
// come up and then to drain. It is generous against the binary's own
// httpShutdownGrace so a test that fails does so because Run is stuck, not
// because the suite misjudged how long a local bind takes.
const serveHTTPStartupGrace = 5 * time.Second

// waitForAddr blocks until addr accepts a connection, fails the test if it
// never does, and returns once it does.
//
// The wait proves Serve began accepting. assemble has already bound the
// listener, so it does not race a temporary port probe against a second bind.
func waitForAddr(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(serveHTTPStartupGrace)
	var lastErr error
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		lastErr = err
		if time.Now().After(deadline) {
			t.Fatalf("%s never accepted a connection: %v", addr, lastErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// appForHTTP assembles an app whose HTTP surface listens on addr. It registers
// a cleanup that closes the app, so a test cannot leak the store.
func appForHTTP(t *testing.T, addr string) *app {
	t.Helper()
	cfg := config.Default()
	cfg.HttpAddr = addr
	a, err := assemble(dbPath(t), cfg)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// runAppInBackground starts Run and returns a function that cancels it and
// collects its result, failing the test if Run does not return in time.
//
// The cancellation and the collection are one helper because they are one
// operation: cancelling without collecting leaves a goroutine that outlives
// the test, and the two written separately at each call site is how the drain
// deadline ends up asserted in one test and forgotten in the rest.
func runAppInBackground(t *testing.T, a *app) (cancelAndWait func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	return func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(serveHTTPStartupGrace + httpShutdownGrace):
			t.Fatalf("Run did not return within %v of cancellation", serveHTTPStartupGrace+httpShutdownGrace)
			return nil
		}
	}
}

// TestServeHTTPStartsAndStopsCleanly proves the HTTP surface is actually
// served by Run: the address app.cfg.HttpAddr names accepts connections once
// Run is in flight, and Run returns when the context is cancelled.
//
// The two are asserted separately because either can be the thing that is
// broken. A Run that started no serve loop would still return on cancellation
// — the two workers end it — so the wait for a connection is what proves
// something is listening, and the return is what proves shutting that
// something down terminates Run instead of hanging on it.
func TestServeHTTPStartsAndStopsCleanly(t *testing.T) {
	a := appForHTTP(t, "127.0.0.1:0")
	addr := a.httpLn.Addr().String()

	wait := runAppInBackground(t, a)
	waitForAddr(t, addr)

	// context.Canceled is the expected answer, not nil: Run's contract is
	// that it reports the cancellation that stopped it so a caller can
	// tell "asked to stop" from "stopped". What must not appear is
	// http.ErrServerClosed dressed as a fault, which an unswallowed
	// ListenAndServe error would surface here.
	if err := wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}
}

// TestServeHTTPServesAssetsEndpoint proves the surface a governance client
// actually uses is wired to the service: GET /api/assets answers 200 with a
// JSON array, not the router's 404 and not a stream of nothing.
//
// The body is required to decode as a JSON array because a handler that
// answered 200 with an empty body would pass a status-only check while
// breaking every client that parses the response.
func TestServeHTTPServesAssetsEndpoint(t *testing.T) {
	a := appForHTTP(t, "127.0.0.1:0")
	addr := a.httpLn.Addr().String()

	wait := runAppInBackground(t, a)
	waitForAddr(t, addr)

	resp, err := http.Get("http://" + addr + "/api/assets")
	if err != nil {
		t.Fatalf("GET /api/assets: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /api/assets = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("Content-Type = %q, want it to contain application/json", got)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("GET /api/assets body is not a JSON array: %v", err)
	}

	if err := wait(); !errors.Is(err, context.Canceled) {
		t.Errorf("Run() = %v, want context.Canceled", err)
	}
}

// TestRunReturnsContextError pins that Run keeps reporting the cancellation
// that stopped it: the HTTP surface must not change the Phase 1.5 contract
// that a caller can tell "asked to stop" from "stopped cleanly".
func TestRunReturnsContextError(t *testing.T) {
	a, err := assemble(dbPath(t), headlessConfig())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() = %v, want context.Canceled", err)
		}
	case <-time.After(serveHTTPStartupGrace):
		t.Fatal("Run() did not return after cancellation")
	}
}

// TestServeHTTPDisabledWhenAddrEmpty proves an empty http_addr disables the
// HTTP surface entirely rather than falling back to the configured default.
func TestServeHTTPDisabledWhenAddrEmpty(t *testing.T) {
	cfg := config.Default()
	cfg.HttpAddr = ""
	a, err := assemble(dbPath(t), cfg)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if a.httpSrv != nil || a.httpLn != nil {
		t.Fatal("empty http_addr created an HTTP server or listener")
	}

	wait := runAppInBackground(t, a)

	if err := wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() with http_addr disabled = %v, want context.Canceled", err)
	}
}

// TestServeHTTPInvalidAddrFailsStartup proves a malformed address is refused
// during config validation, before the store is opened or a listener is
// created.
func TestServeHTTPInvalidAddrFailsStartup(t *testing.T) {
	cfg := config.Default()
	cfg.HttpAddr = "invalid addr without port"
	if _, err := assemble(dbPath(t), cfg); err == nil {
		t.Fatal("assemble: expected an error for an unbindable http_addr, got nil")
	}
}

// TestServeHTTPServesEveryEndpoint is the lightweight form of Gate 2: every
// documented URL answers over a real connection, and none of them answers 501
// not_implemented (the stub that would mean a route was registered without a
// handler behind it).
//
// The endpoints are driven with bodies that are actually legal for them — a
// governance PATCH on the selfcheck asset, a POST task on that same asset —
// because a request refused with 400 still proves the route and the handler
// exist, while one refused with 501 proves nothing useful. The SSE endpoint is
// the exception: it is a long-lived stream, so it is asserted on its status
// and headers and its body closed immediately, which is also what keeps the
// drain from waiting on it.
func TestServeHTTPServesEveryEndpoint(t *testing.T) {
	a := appForHTTP(t, "127.0.0.1:0")
	addr := a.httpLn.Addr().String()
	if err := a.selfcheck(context.Background()); err != nil {
		t.Fatalf("selfcheck: %v", err)
	}

	wait := runAppInBackground(t, a)
	waitForAddr(t, addr)

	base := "http://" + addr
	for _, tc := range []struct {
		name   string
		method string
		url    string
		body   string
		want   int
	}{
		{"asset list", "GET", "/api/assets", "", 200},
		{"asset detail", "GET", "/api/assets/" + selfcheckAssetID, "", 200},
		{"asset detail unknown", "GET", "/api/assets/does-not-exist", "", 404},
		{"asset governance", "PATCH", "/api/assets/" + selfcheckAssetID, `{"agent_visible":true}`, 200},
		{"task create", "POST", "/api/tasks", `{"task_id":"_smoke","asset_id":"_selfcheck","type":"probe","agent_role":"prober"}`, 201},
		{"task detail", "GET", "/api/tasks/_smoke", "", 200},
		{"task detail unknown", "GET", "/api/tasks/does-not-exist", "", 404},
		{"audit log", "GET", "/api/audit", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			req, err := http.NewRequest(tc.method, base+tc.url, body)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.url, err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusNotImplemented {
				t.Errorf("%s %s = 501, want a real answer (no stub may survive)", tc.method, tc.url)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("%s %s = %d, want %d", tc.method, tc.url, resp.StatusCode, tc.want)
			}
		})
	}

	// The stream is checked separately: it never completes on its own, so
	// it is requested, its headers asserted, and its body closed at once.
	t.Run("event stream", func(t *testing.T) {
		resp, err := http.Get(base + "/api/events")
		if err != nil {
			t.Fatalf("GET /api/events: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/events = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
			t.Errorf("Content-Type = %q, want it to contain text/event-stream", got)
		}
	})

	if err := wait(); !errors.Is(err, context.Canceled) {
		t.Errorf("Run() = %v, want context.Canceled", err)
	}
}

// TestServeHTTPDrainsThenReturns proves the drain is bounded rather than
// infinite: an SSE subscription is a request that never completes on its own,
// so leaving a body open until the client gives up would hold Run open for as
// long as the client kept the connection, and the process would never exit.
//
// The assertion is that Run returns, and returns promptly — within
// httpShutdownGrace plus slack — rather than that it returns nil. The timeout
// is deliberately logged rather than raised as an error, because a shutdown
// the operator asked for is not made worse by a client that has not left yet.
func TestServeHTTPDrainsThenReturns(t *testing.T) {
	a := appForHTTP(t, "127.0.0.1:0")
	addr := a.httpLn.Addr().String()

	wait := runAppInBackground(t, a)
	waitForAddr(t, addr)

	// The stream is opened and deliberately never closed: the handler holds
	// it until the request context is cancelled or the drain gives up, and
	// closing it here would remove the only thing the timeout exists for.
	// The response is discarded rather than read, because reading would
	// block on frames the server is not sending.
	resp, err := http.Get("http://" + addr + "/api/events")
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/events = %d, want 200", resp.StatusCode)
	}

	start := time.Now()
	if err := wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}
	// The drain is allowed to end early — a real client that hung up, or
	// the deadline — so the upper bound is the grace plus the time the
	// cancel has to travel through the workers, and the lower bound is
	// simply "did not return instantly", which would mean Shutdown was
	// never called at all.
	if elapsed := time.Since(start); elapsed > httpShutdownGrace+2*time.Second {
		t.Errorf("Run took %v to return, want it bounded by the %v drain", elapsed, httpShutdownGrace)
	}
}

// TestServeHTTPClosesListenerOnFailure covers the one serve error Run is
// meant to report. A server whose listener is closed underneath it returns a
// real error from Serve, and serveHTTP must report it as its result: a socket
// that dies while the process believes it is serving is a fault the operator
// has to see, and swallowing it would leave a plane that answers nothing while
// reporting healthy.
//
// It calls serveHTTP directly rather than going through Run because Run waits
// for the workers, which only end when the caller's context does — so a
// listener that dies without a cancellation would leave Run waiting for a
// shutdown nobody asked for. That is Run's correct behaviour (the workers are
// still sweeping and still have work), and it means the propagation itself has
// to be observed at the layer where the result is produced.
func TestServeHTTPClosesListenerOnFailure(t *testing.T) {
	a := appForHTTP(t, "127.0.0.1:0")
	addr := a.httpLn.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	waitForAddr(t, addr)

	// Closing the listener is what makes Serve fail: the accept loop exits
	// with an error that is not http.ErrServerClosed, because nothing asked
	// the server to shut down — the socket simply went away. Shutdown's own
	// close produces the sentinel and is filtered; this does not.
	if err := a.httpLn.Close(); err != nil {
		t.Fatalf("close the listener: %v", err)
	}

	// serveHTTP is given a context that is never cancelled, so the select
	// that watches it falls through to Serve's own return rather than
	// reaching the drain. The one-shot error is what the surface reports.
	err := a.serveHTTP(ctx)
	if err == nil {
		t.Fatal("serveHTTP() = nil, want the accept-loop failure to propagate")
	}
	if errors.Is(err, http.ErrServerClosed) {
		t.Errorf("serveHTTP() = %v, want a real fault rather than the deliberate-close sentinel", err)
	}
}

// TestServeHTTPSwallowsDeliberateClose pins the counterpart: when the caller's
// context is cancelled, the listener that Serve was using is closed by
// serveHTTP's own drain, and the http.ErrServerClosed that produces must not
// reach the caller as a fault. Without this the previous test would pass and
// every ordinary shutdown would exit 1.
func TestServeHTTPSwallowsDeliberateClose(t *testing.T) {
	a := appForHTTP(t, "127.0.0.1:0")
	addr := a.httpLn.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.serveHTTP(ctx) }()

	waitForAddr(t, addr)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serveHTTP() = %v, want nil for a deliberate close", err)
		}
	case <-time.After(httpShutdownGrace + serveHTTPStartupGrace):
		t.Fatal("serveHTTP() did not return after cancellation")
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
