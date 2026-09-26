package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// defaultArchiveInterval is the cadence the Archiver falls back to when the
// caller passes a non-positive interval. Retention is a daily-scale
// operation: the live window is measured in days, so nothing changes between
// two sweeps a few seconds apart, and the second sweep would replay the same
// aged rows against the database for no benefit.
const defaultArchiveInterval = 24 * time.Hour

// Archiver periodically applies the audit retention policy.
//
// Like the Reclaimer it drives, it holds no rules: what counts as expired,
// which rows are agent rows, and where archived rows go all live in
// service.SweepAudit. The ticker only decides how often that sweep runs.
//
// A sweep failure is recorded rather than fatal: losing a retention cycle
// costs a few rows that stay live a little longer, and taking the control
// plane down over it would break everything else the process does. The
// failure stays visible through LastErr until a new run replaces it.
type Archiver struct {
	svc         *service.Service
	every       time.Duration
	retainDays  int
	archiveDays int

	mu      sync.Mutex
	lastErr error
}

// NewArchiver returns an Archiver that sweeps every `every`.
//
// A non-positive interval falls back to defaultArchiveInterval rather than
// to the Reclaimer's cadence: retention is a daily-scale operation, and
// sweeping it every 30 seconds would replay the same aged rows against the
// database all day for no benefit.
//
// The retention windows are stored exactly as given — a zero here is not
// silently replaced with a default, because a caller that asked for a zero
// day retention window asked to delete the whole audit log on every sweep.
// Run refuses it instead.
func NewArchiver(svc *service.Service, every time.Duration, retainDays, archiveDays int) *Archiver {
	if every <= 0 {
		every = defaultArchiveInterval
	}
	return &Archiver{svc: svc, every: every, retainDays: retainDays, archiveDays: archiveDays}
}

// Run blocks until ctx is cancelled, sweeping once per interval. It returns
// ctx.Err() on cancellation; a sweep error never ends the loop.
//
// A non-positive retention window is refused before the first tick rather
// than silently replaced with a default: SweepAudit rejects it too, and a
// ticker that cannot ever succeed should not look like it is working.
//
// The first sweep runs before the first wait, because the retention policy
// applies to rows that aged out while the process was down: a wait before
// the first look would leave those rows live for a whole cycle after a
// restart.
//
// lastErr describes the current run, so it starts empty: an error reported
// by a previous run must not outlive the run that produced it.
func (a *Archiver) Run(ctx context.Context) error {
	if a.retainDays <= 0 || a.archiveDays <= 0 {
		return fmt.Errorf("queue: archiver: retention window must be > 0 (retain=%d, archive=%d): %w",
			a.retainDays, a.archiveDays, model.ErrArgument)
	}

	a.setLastErr(nil)

	ticker := time.NewTicker(a.every)
	defer ticker.Stop()

	for {
		a.sweep(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// LastErr returns the most recent sweep failure, or nil. It is mutex-guarded
// because Run and LastErr are meant to be called from different goroutines
// (an operator endpoint reading health while the loop runs).
//
// The recorded error is sticky — a later success does not erase it — because
// a health check may well poll on a slower cadence than this one, and a
// missed retention cycle must not become invisible just because the next one
// happened to work. Only a new Run session clears it.
func (a *Archiver) LastErr() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastErr
}

// sweep runs one retention pass and records a genuine store failure.
//
// A failure caused by the caller's own cancellation is not recorded: the
// sweep was cut short because the process is going away, not because the
// store is broken, and reporting it would cry wolf on every shutdown. An
// aborted sweep is safe to abandon — the next tick applies the same policy to
// whatever this one did not reach.
func (a *Archiver) sweep(ctx context.Context) {
	archived, deleted, err := a.svc.SweepAudit(ctx, a.retainDays, a.archiveDays)
	if err == nil || ctx.Err() != nil {
		return
	}
	a.setLastErr(fmt.Errorf("queue: sweep audit (%d archived, %d deleted): %w", archived, deleted, err))
}

// setLastErr stores err under the lock. Run and LastErr share it from
// different goroutines, so every write goes through the same mutex.
func (a *Archiver) setLastErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastErr = err
}
