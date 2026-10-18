// Package service is the single business-logic layer of the control plane.
// Every transport — REST, MCP and SSE — projects the same rules through this
// package, and it holds no transport or protocol knowledge of its own.
package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// Service is the business-logic facade every transport talks to. It owns the
// rules; the store owns the SQL, the event bus owns the fan-out and the
// handlers own their protocols. A Service is safe for concurrent use as long
// as its collaborators are, which store.Store and events.Bus both are.
type Service struct {
	// st is the persistence layer. It never sees a business rule.
	st         *store.Store
	ttsRuntime *ttsRuntime
	// deliveryRoot is configured before serving HTTP and never changes after startup.
	deliveryRoot string
	// maxAttempts caps lease recoveries per task; see SetMaxAttempts.
	maxAttempts int
	// ingest is configured once at startup by ConfigureIngest.
	ingest ingestConfig

	// prober inspects preview media streams; defaults to preview.DefaultProber.
	prober   preview.MediaProber
	proberMu sync.RWMutex

	// bus is the optional fan-out sink for state-change events. It is nil
	// until SetBus is called, which keeps the service usable (and testable)
	// before the Phase 2 SSE handler exists. Every publish path checks it.
	bus   *events.Bus
	busMu sync.RWMutex

	// ctrl tracks at most one in-flight execution control poll per execution.
	// The wait itself happens outside any SQLite transaction.
	ctrlMu         sync.Mutex
	ctrl           map[string]*controlSlot
	controlWait    time.Duration
	controlWaitSet bool
	controlHook    func()

	// afterEarlyAutoCheck is a test seam: startWorkflow calls it between the
	// early Rule A check and its transaction, the window the in-transaction
	// check exists for. It is nil outside tests.
	afterEarlyAutoCheck func()
	// beforePreviewTx runs in startPreview after every pre-transaction reuse
	// check missed and before the transaction opens: the window in which another
	// start can create the job. nil outside tests.
	beforePreviewTx func()
	// beforeAutoTx runs in AutoConfirmScene and AutoApproveNarration between the
	// early replay check and the transaction, the window the in-transaction
	// replay check exists for. nil outside tests.
	beforeAutoTx func()
	// encodeJSON encodes the stored facts of a review; nil means json.Marshal.
	// Tests replace it to prove a failed encoding writes nothing.
	encodeJSON func(any) ([]byte, error)
	// afterScoreCollect runs between ScoreAutoRun's file reads and its
	// transaction; tests use it to change the world in that gap.
	afterScoreCollect func()
	// beforeReconcileTx runs after reconcileHumanFinished has seen a finished
	// run and before it locks the overlay. Tests commit a newer auto_version
	// or a new current revision in that window. nil outside tests.
	beforeReconcileTx func()

	// clock is the auto-run clock (deadlines, decision timeouts). nil means
	// the wall clock; SetClock lets tests move time without sleeping.
	clock   func() time.Time
	clockMu sync.RWMutex
}

// New returns a Service backed by st with no bus attached. SetBus attaches
// one later; until then publishes are no-ops.
func New(st *store.Store) *Service {
	return &Service{st: st, prober: preview.DefaultProber}
}

// SetMediaProber configures the prober used for preview media validation.
func (s *Service) SetMediaProber(p preview.MediaProber) {
	s.proberMu.Lock()
	s.prober = p
	s.proberMu.Unlock()
}

// MediaProber returns the current prober used for preview media validation.
func (s *Service) MediaProber() preview.MediaProber {
	s.proberMu.RLock()
	p := s.prober
	s.proberMu.RUnlock()
	if p == nil {
		return preview.DefaultProber
	}
	return p
}

// SetBus attaches the bus that state-change events are published to. Passing
// the bus a second time replaces the previous one, so a restart or a test
// re-wire is not an error. Passing nil detaches it again.
func (s *Service) SetBus(b *events.Bus) {
	s.busMu.Lock()
	s.bus = b
	s.busMu.Unlock()
}

// Bus returns the attached bus, or nil when none is attached.
func (s *Service) Bus() *events.Bus {
	s.busMu.RLock()
	b := s.bus
	s.busMu.RUnlock()
	return b
}

// publish fans one envelope out to every subscriber of name.
//
// A nil bus is the supported configuration, so publishing is a no-op there
// rather than a panic: the write path must succeed whether or not anybody is
// listening for it.
func (s *Service) publish(name string, payload any) {
	b := s.Bus()
	if b == nil {
		return
	}
	b.Publish(events.Envelope{Name: name, Payload: payload})
}

// audit records one governance or system action and deliberately swallows
// the failure, logging it instead.
//
// An audit failure must never break the write path it observed: the state
// change is already committed by the time this runs, so returning the error
// would turn a successful operation into a reported failure the caller
// cannot undo. The log record keeps the loss visible.
func (s *Service) audit(ctx context.Context, actor, action, target, detail string) {
	if err := s.st.WriteAudit(ctx, model.AuditLog{
		Actor:  actor,
		Action: action,
		Target: target,
		Detail: detail,
	}); err != nil {
		slog.Default().Error("audit write failed",
			"actor", actor, "action", action, "target", target, "err", err)
		return
	}
	s.publish("audit.changed", map[string]any{})
}

// SetClock replaces the clock the auto-run driver and its guards read. A nil
// function restores the wall clock.
func (s *Service) SetClock(f func() time.Time) {
	s.clockMu.Lock()
	s.clock = f
	s.clockMu.Unlock()
}

func (s *Service) autoNow() time.Time {
	s.clockMu.RLock()
	f := s.clock
	s.clockMu.RUnlock()
	if f != nil {
		return f().UTC()
	}
	return time.Now().UTC()
}
