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
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// Service is the business-logic facade every transport talks to. It owns the
// rules; the store owns the SQL, the event bus owns the fan-out and the
// handlers own their protocols. A Service is safe for concurrent use as long
// as its collaborators are, which store.Store and events.Bus both are.
type Service struct {
	// st is the persistence layer. It never sees a business rule.
	st *store.Store
	// deliveryRoot is configured before serving HTTP and never changes after startup.
	deliveryRoot string
	// maxAttempts caps lease recoveries per task; see SetMaxAttempts.
	maxAttempts int
	// ingest is configured once at startup by ConfigureIngest.
	ingest ingestConfig

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
}

// New returns a Service backed by st with no bus attached. SetBus attaches
// one later; until then publishes are no-ops.
func New(st *store.Store) *Service {
	return &Service{st: st}
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
	}
}
