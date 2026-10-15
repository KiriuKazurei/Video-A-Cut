package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// defaultAutoDriveInterval is how often live auto runs are looked at. Every
// step the driver takes is idempotent and bounded, so the cadence only decides
// how quickly a finished worker stage is noticed.
const defaultAutoDriveInterval = 5 * time.Second

// maxAutoBackoff caps how far consecutive tick failures stretch the wait. A
// database that is down is asked about once a minute, not every five seconds.
const maxAutoBackoff = time.Minute

// AutoTicker is the part of the service the driver needs.
type AutoTicker interface {
	AutoTick(ctx context.Context) error
}

// AutoDriver periodically advances auto runs. It holds no rules: what a run
// may do next, the clock, the caps and the transactions all live in
// service.AutoTick. A tick failure (an error or a panic) is logged and recorded,
// never fatal: the next tick looks at the same database state. Consecutive
// failures stretch the wait up to a minute (see nextWait); the first success
// restores the normal cadence and clears LastErr.
type AutoDriver struct {
	svc   AutoTicker
	every time.Duration
	fails int // consecutive failed ticks; only Run's goroutine touches it

	mu      sync.Mutex
	lastErr error
}

// NewAutoDriver returns a driver that ticks every `every` (a non-positive
// interval falls back to the default).
func NewAutoDriver(svc AutoTicker, every time.Duration) *AutoDriver {
	if every <= 0 {
		every = defaultAutoDriveInterval
	}
	return &AutoDriver{svc: svc, every: every}
}

// Run ticks until ctx is cancelled and returns ctx.Err(). The first tick runs
// before the first wait: runs that were mid-flight when the process stopped
// continue at once.
func (d *AutoDriver) Run(ctx context.Context) error {
	d.setLastErr(nil)
	d.fails = 0
	for {
		d.tick(ctx)
		timer := time.NewTimer(d.nextWait())
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// nextWait is the pause before the next tick: the interval after a success,
// doubled for each consecutive failure and capped at maxAutoBackoff (never
// shorter than the interval itself).
func (d *AutoDriver) nextWait() time.Duration {
	wait := d.every
	for i := 0; i < d.fails && wait < maxAutoBackoff; i++ {
		wait *= 2
	}
	if wait > maxAutoBackoff && d.every <= maxAutoBackoff {
		wait = maxAutoBackoff
	}
	return wait
}

// LastErr returns the failure of the most recent tick, or nil when that tick
// succeeded.
func (d *AutoDriver) LastErr() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastErr
}

func (d *AutoDriver) tick(ctx context.Context) {
	err := d.safeTick(ctx)
	switch {
	case ctx.Err() != nil:
		// Shutting down: a cancelled tick is not a failure.
	case err != nil:
		d.fails++
		d.setLastErr(err)
		slog.Error("auto driver tick failed", "consecutive_failures", d.fails, "error", err)
	default:
		if d.fails > 0 {
			slog.Info("auto driver recovered", "after_failures", d.fails)
		}
		d.fails = 0
		d.setLastErr(nil)
	}
}

// safeTick runs one tick and turns a panic into an error: a bug in one run's
// step must not take the control plane down with it.
func (d *AutoDriver) safeTick(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("queue: auto tick panicked: %v", r)
		}
	}()
	return d.svc.AutoTick(ctx)
}

func (d *AutoDriver) setLastErr(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastErr = err
}
