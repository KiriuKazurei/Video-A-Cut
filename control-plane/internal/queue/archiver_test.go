package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/queue"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// writeAgentAudit appends one agent audit row and returns its id. WriteAudit
// stamps its own created_at, so the caller must backdate the row afterwards
// to put it outside the retention window.
func writeAgentAudit(t *testing.T, s *store.Store, actor string) int64 {
	t.Helper()
	if err := s.WriteAudit(context.Background(), model.AuditLog{
		Actor:  actor,
		Action: "task.claim",
		Target: "t_001",
	}); err != nil {
		t.Fatalf("WriteAudit(%s): %v", actor, err)
	}
	var id int64
	if err := s.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
		t.Fatalf("read back audit id: %v", err)
	}
	return id
}

// countRows counts the rows currently in table, for the assertions that care
// about which table a sweep left a row in rather than what the row contains.
func countRows(t *testing.T, s *store.Store, table string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return n
}

// TestArchiverSweepsOnTick is the reason the Archiver exists at all: an agent
// audit row that has aged out of the live window must actually leave the live
// log and land in the archive, driven by the ticker rather than by an
// operator calling SweepAudit by hand.
//
// The row is written now and then backdated 40 days, which is what a row the
// operator saw 40 days ago looks like to the 30 day window. The loop runs for
// 150ms against a 20ms interval — several full ticks — and by the time the
// context ends the live table must be empty and the archive must hold exactly
// that one row. LastErr must still be nil: the sweep succeeded, and the run
// is stopped by the context, not by a failure.
func TestArchiverSweepsOnTick(t *testing.T) {
	st := newStore(t)
	svc := service.New(st)

	id := writeAgentAudit(t, st, "agent:narrator-01")
	if _, err := st.DB().Exec(`update audit_logs set created_at = ? where id = ?`,
		time.Now().UTC().AddDate(0, 0, -40), id); err != nil {
		t.Fatalf("backdate audit row: %v", err)
	}

	a := queue.NewArchiver(svc, 20*time.Millisecond, 30, 30)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := a.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: %v, want context.DeadlineExceeded", err)
	}
	if lastErr := a.LastErr(); lastErr != nil {
		t.Errorf("LastErr = %v, want nil (a healthy sweep is not a failure)", lastErr)
	}
	if n := countRows(t, st, "audit_logs"); n != 0 {
		t.Errorf("audit_logs holds %d rows, want 0 (the aged agent row left the live log)", n)
	}
	if n := countRows(t, st, "audit_logs_archive"); n != 1 {
		t.Errorf("audit_logs_archive holds %d rows, want 1 (the aged agent row was archived)", n)
	}
}

// TestArchiverStopsOnCancel verifies the loop is a well-behaved background
// worker: it returns the moment it is told to stop, and it reports
// context.Canceled rather than some error of its own invention, so a
// supervisor can tell a deliberate shutdown apart from a crash.
//
// The cancel is issued from another goroutine after the loop has had time to
// start — a loop that only noticed cancellation on the next tick would make
// shutdown wait a full interval for nothing.
func TestArchiverStopsOnCancel(t *testing.T) {
	st := newStore(t)
	svc := service.New(st)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() { done <- queue.NewArchiver(svc, 10*time.Millisecond, 30, 30).Run(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run: %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of cancellation")
	}
}

// TestArchiverRejectsBadWindows pins the argument rule: the ticker must not
// start at all when the retention window cannot produce a working sweep.
//
// SweepAudit rejects the same values, so allowing Run to proceed would give
// the operator a loop that reports nothing wrong while doing nothing useful
// for its entire life — the failure would only surface as a LastErr nobody
// was watching. Both directions are covered: a zero retain window means
// "expire everything" and a negative archive window is simply malformed.
func TestArchiverRejectsBadWindows(t *testing.T) {
	st := newStore(t)
	svc := service.New(st)

	for name, a := range map[string]*queue.Archiver{
		"zero retain": queue.NewArchiver(svc, time.Second, 0, 30),
		"neg archive": queue.NewArchiver(svc, time.Second, 30, -1),
	} {
		t.Run(name, func(t *testing.T) {
			err := a.Run(context.Background())
			if !errors.Is(err, model.ErrArgument) {
				t.Fatalf("Run: %v, want model.ErrArgument", err)
			}
		})
	}
}

// TestArchiverRecordsSweepFailure is the anti-crash rule of the ticker: the
// sweep talks to a store that can fail, and a failing store must not take
// the control plane down with it.
//
// The database is closed while the loop runs, so every sweep fails. The loop
// must still keep running until the context ends — returning early on a store
// error would silently stop retention the moment it matters most — and the
// failure must be visible through LastErr, because a tick that quietly did
// nothing is indistinguishable from a healthy one until an operator looks.
func TestArchiverRecordsSweepFailure(t *testing.T) {
	st := newStore(t)
	svc := service.New(st)

	if err := st.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	a := queue.NewArchiver(svc, 20*time.Millisecond, 30, 30)
	err := a.Run(runCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: %v, want context.DeadlineExceeded (a store failure must not end the loop)", err)
	}
	// LastErr is read after Run returned, so no other goroutine is still
	// writing to it; it is guarded either way because that is the contract
	// operators rely on.
	if lastErr := a.LastErr(); lastErr == nil {
		t.Error("LastErr = nil, want the store failure recorded")
	}
}

// TestNewArchiverDefaultsNonPositiveInterval pins the config-safety rule: a
// zero or negative interval comes from a half-written config file, and it
// must not produce either a panicking constructor or a hot loop.
//
// The assertion is indirect because `every` is unexported: the archiver is
// driven with such an interval for 60ms and must still exit cleanly on the
// context with no sweep failure. A degenerate interval could not do both —
// and the default is the archive cadence, not the reclaimer's 30 seconds,
// because nothing about retention benefits from being swept that often.
func TestNewArchiverDefaultsNonPositiveInterval(t *testing.T) {
	st := newStore(t)
	svc := service.New(st)

	for name, every := range map[string]time.Duration{
		"zero":     0,
		"negative": -time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()

			a := queue.NewArchiver(svc, every, 30, 30)
			if err := a.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run: %v, want context.DeadlineExceeded", err)
			}
			if lastErr := a.LastErr(); lastErr != nil {
				t.Errorf("LastErr = %v, want nil", lastErr)
			}
		})
	}
}

// TestArchiverKeepsFreshRows is the other half of the retention policy: a
// sweep must only ever touch rows that have actually aged out.
//
// An agent row written a day ago sits well inside the 30 day window. The
// loop runs for 80ms against a 20ms interval, so several sweeps get a chance
// to destroy it, and all of them must leave the live table exactly as they
// found it — an archiver that deleted young rows would empty the audit log
// the operators read.
func TestArchiverKeepsFreshRows(t *testing.T) {
	st := newStore(t)
	svc := service.New(st)

	id := writeAgentAudit(t, st, "agent:narrator-01")
	if _, err := st.DB().Exec(`update audit_logs set created_at = ? where id = ?`,
		time.Now().UTC().AddDate(0, 0, -1), id); err != nil {
		t.Fatalf("backdate audit row: %v", err)
	}

	a := queue.NewArchiver(svc, 20*time.Millisecond, 30, 30)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	if err := a.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: %v, want context.DeadlineExceeded", err)
	}
	if lastErr := a.LastErr(); lastErr != nil {
		t.Errorf("LastErr = %v, want nil", lastErr)
	}
	if n := countRows(t, st, "audit_logs"); n != 1 {
		t.Errorf("audit_logs holds %d rows, want 1 (a row inside its window survives every sweep)", n)
	}
	if n := countRows(t, st, "audit_logs_archive"); n != 0 {
		t.Errorf("audit_logs_archive holds %d rows, want 0 (nothing was archived)", n)
	}
}
