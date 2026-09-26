// Package queue owns the control plane's periodic drivers: loops that keep
// recovery work running for as long as the process lives.
//
// It deliberately holds no rules. What a tick must do — which leases count as
// expired, what a recovered task looks like — belongs to the layer that owns
// that state machine, and duplicating it here would create a second
// definition that can disagree with the first. This package only decides
// when, catches the failure so it stays visible, and hands control back when
// the process is asked to shut down.
package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// defaultInterval is the cadence the Reclaimer falls back to when the caller
// passes a non-positive interval. A zero in a config file must degrade to a
// sane sweep cadence — not to a tight loop burning CPU, and not to recovery
// being silently switched off.
const defaultInterval = 30 * time.Second

// Reclaimer periodically sweeps expired task leases back into the queue.
//
// It owns no rules: the sweep itself lives in service.RequeueExpiredLeases.
// The ticker only decides how often that sweep runs and keeps the process
// alive between ticks. A sweep failure is recorded rather than fatal — an
// agent crash must not take the control plane down with it — so a transient
// store error costs one missed cycle and is visible through LastErr.
type Reclaimer struct {
	svc     *service.Service
	every   time.Duration
	mu      sync.Mutex
	lastErr error
}

// New returns a Reclaimer that sweeps every `every`. A non-positive interval
// falls back to defaultInterval, so a zero value in a config file degrades to
// a sane cadence instead of spinning the CPU.
func New(svc *service.Service, every time.Duration) *Reclaimer {
	if every <= 0 {
		every = defaultInterval
	}
	return &Reclaimer{svc: svc, every: every}
}

// Run blocks until ctx is cancelled, sweeping once per interval. It returns
// ctx.Err() on cancellation; a sweep error never ends the loop.
//
// The first sweep runs before the first wait rather than a whole interval
// later. A restart has to recover the leases that lapsed while the process
// was down — those tasks are stuck in claimed forever — and making an
// operator wait a full cycle for the first look at them would leave exactly
// the stuck tasks the sweep exists for sitting in running for no reason.
//
// lastErr describes the current run, so it starts empty: an error reported
// by a previous run must not outlive the run that produced it.
func (r *Reclaimer) Run(ctx context.Context) error {
	r.setLastErr(nil)

	ticker := time.NewTicker(r.every)
	defer ticker.Stop()

	for {
		r.sweep(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Interval returns the cadence sweeps run at after any correction New
// applied.
//
// It exists so a caller that derives this value from an operator setting —
// the composition root turns lease_seconds into the sweep cadence — can tell
// what cadence the reclaimer will actually use. Without it, a non-positive
// interval would be corrected to defaultInterval in total silence: the loop
// would keep running happily on a cadence nobody chose and nothing anywhere
// would report the substitution.
func (r *Reclaimer) Interval() time.Duration {
	return r.every
}

// LastErr returns the most recent sweep failure, or nil. It is mutex-guarded
// because Run and LastErr are meant to be called from different goroutines
// (an operator endpoint reading health while the loop runs).
//
// The recorded error is sticky — a later success does not erase it — because
// a health check may well poll on a slower cadence than the ticker, and a
// recovery cycle that was missed must not become invisible just because the
// next one happened to work. Only a new Run session clears it.
func (r *Reclaimer) LastErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

// sweep runs one recovery pass and records a genuine store failure.
//
// A failure caused by the caller's own cancellation is not recorded: the
// sweep was cut short because the process is going away, not because the
// store is broken, and reporting it would cry wolf on every shutdown. An
// aborted sweep is safe to abandon — the sweep is idempotent, and the next
// tick picks up whatever this one did not reach.
func (r *Reclaimer) sweep(ctx context.Context) {
	_, err := r.svc.RequeueExpiredLeases(ctx)
	if err == nil || ctx.Err() != nil {
		return
	}
	r.setLastErr(fmt.Errorf("queue: reclaim expired leases: %w", err))
}

// setLastErr stores err under the lock. Run and LastErr share it from
// different goroutines, so every write goes through the same mutex.
func (r *Reclaimer) setLastErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr = err
}
