package queue

import (
	"context"
	"sync"
	"time"
)

// defaultAutoDriveInterval is how often live auto runs are looked at. Every
// step the driver takes is idempotent and bounded, so the cadence only decides
// how quickly a finished worker stage is noticed.
const defaultAutoDriveInterval = 5 * time.Second

// AutoTicker is the part of the service the driver needs.
type AutoTicker interface {
	AutoTick(ctx context.Context) error
}

// AutoDriver periodically advances auto runs. It holds no rules: what a run
// may do next, the clock, the caps and the transactions all live in
// service.AutoTick. A tick failure is recorded, never fatal, the same as the
// Reclaimer and the Archiver: the next tick looks at the same database state.
type AutoDriver struct {
	svc   AutoTicker
	every time.Duration

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
	ticker := time.NewTicker(d.every)
	defer ticker.Stop()
	for {
		d.tick(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// LastErr returns the most recent tick failure, sticky until the next Run.
func (d *AutoDriver) LastErr() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastErr
}

func (d *AutoDriver) tick(ctx context.Context) {
	if err := d.svc.AutoTick(ctx); err != nil && ctx.Err() == nil {
		d.setLastErr(err)
	}
}

func (d *AutoDriver) setLastErr(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastErr = err
}
