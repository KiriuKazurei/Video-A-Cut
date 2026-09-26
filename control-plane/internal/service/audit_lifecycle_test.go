package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// sweepFixture is one Service plus the store behind it. The sweep's results
// are not fully visible through the service surface — ListAudit reads only
// the live log, and the archive table has no reader — so the tests inspect
// the database directly to assert what the sweep moved. Holding both in one
// fixture keeps every test from re-wiring the same pair.
type sweepFixture struct {
	svc *service.Service
	st  *store.Store
}

// newSweepFixture opens a fresh database and a Service over it. It is kept
// separate from newService because the sweep tests must seed rows through the
// store directly: WriteAudit is the only way to produce an audit row, and it
// stamps its own created_at, so the test has to age rows afterwards.
func newSweepFixture(t *testing.T) sweepFixture {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/vac.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return sweepFixture{svc: service.New(st), st: st}
}

// writeAuditRow appends one audit entry through the store's own write path,
// so the rows under test have exactly the shape every service write produces.
func (f sweepFixture) writeAuditRow(t *testing.T, actor string) int64 {
	t.Helper()
	if err := f.st.WriteAudit(context.Background(), model.AuditLog{
		Actor:  actor,
		Action: "task.claim",
		Target: "t_001",
	}); err != nil {
		t.Fatalf("WriteAudit(%s): %v", actor, err)
	}
	var id int64
	if err := f.st.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
		t.Fatalf("read back audit id: %v", err)
	}
	return id
}

// ageAuditRow rewrites one audit row's created_at to an instant daysInPast
// days ago, which is how a test manufactures a row the retention window has
// already passed. It fails the test unless exactly the one row moved.
func (f sweepFixture) ageAuditRow(t *testing.T, id int64, daysInPast int) {
	t.Helper()
	res, err := f.st.DB().Exec(`UPDATE audit_logs SET created_at = ? WHERE id = ?`,
		time.Now().UTC().AddDate(0, 0, -daysInPast), id)
	if err != nil {
		t.Fatalf("age audit row %d: %v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		t.Fatalf("age audit row %d: rows affected: %v", id, err)
	} else if n != 1 {
		t.Fatalf("age audit row %d: affected %d rows, want 1", id, n)
	}
}

// ageArchivedRow backdates the archived_at timestamp of an already-archived
// row. The archive window is measured from archived_at, so this is how a test
// produces a row that has spent its archive window in the archive table.
func (f sweepFixture) ageArchivedRow(t *testing.T, id int64, daysInPast int) {
	t.Helper()
	res, err := f.st.DB().Exec(`UPDATE audit_logs_archive SET archived_at = ? WHERE id = ?`,
		time.Now().UTC().AddDate(0, 0, -daysInPast), id)
	if err != nil {
		t.Fatalf("age archived row %d: %v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		t.Fatalf("age archived row %d: rows affected: %v", id, err)
	} else if n != 1 {
		t.Fatalf("age archived row %d: affected %d rows, want 1", id, n)
	}
}

// archiveRows reports how many rows currently sit in the archive table.
func (f sweepFixture) archiveRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.st.DB().QueryRow(`SELECT COUNT(*) FROM audit_logs_archive`).Scan(&n); err != nil {
		t.Fatalf("count archive rows: %v", err)
	}
	return n
}

// inArchive reports whether id is physically present in the archive table,
// which is the difference between "counted as archived" and "actually moved".
func (f sweepFixture) inArchive(t *testing.T, id int64) bool {
	t.Helper()
	var got bool
	if err := f.st.DB().QueryRow(
		`SELECT EXISTS(SELECT 1 FROM audit_logs_archive WHERE id = ?)`, id).Scan(&got); err != nil {
		t.Fatalf("check archive row %d: %v", id, err)
	}
	return got
}

// TestSweepAuditArchivesAgentRows pins the whole retention rule in one sweep:
// aged-out agent rows are MOVED into the archive, the remaining aged-out rows
// are deleted outright, and nothing inside the window is touched.
func TestSweepAuditArchivesAgentRows(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()

	// Four rows, three of them outside the 30-day window. The agent rows must
	// be picked out by actor and age, not by insertion order.
	agentOld1 := f.writeAuditRow(t, "agent:narrator-01")
	agentOld2 := f.writeAuditRow(t, "agent:narrator-05")
	systemOld := f.writeAuditRow(t, "human:webui")
	agentFresh := f.writeAuditRow(t, "agent:narrator-01")
	for _, id := range []int64{agentOld1, agentOld2, systemOld} {
		f.ageAuditRow(t, id, 40)
	}
	f.ageAuditRow(t, agentFresh, 1)

	archived, deleted, err := f.svc.SweepAudit(ctx, 30, 30)
	if err != nil {
		t.Fatalf("SweepAudit: %v", err)
	}
	if archived != 2 {
		t.Fatalf("archived = %d, want 2 (the two 40-day-old agent rows)", archived)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (the 40-day-old non-agent row)", deleted)
	}

	// The live log keeps exactly the row that is still inside the window.
	remaining, err := f.svc.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("ListAudit returned %d entries, want 1 (only the 1-day-old row): %+v", len(remaining), remaining)
	}
	if remaining[0].ID != agentFresh {
		t.Errorf("surviving audit row id = %d, want %d", remaining[0].ID, agentFresh)
	}

	// Both moved rows are physically in the archive, with their ids intact.
	if n := f.archiveRows(t); n != 2 {
		t.Errorf("archive rows = %d, want 2", n)
	}
	for _, id := range []int64{agentOld1, agentOld2} {
		if !f.inArchive(t, id) {
			t.Errorf("audit row %d was counted as archived but is not in the archive table", id)
		}
	}
}

// TestSweepAuditKeepsFreshRows verifies a sweep on a log where nothing has
// aged out is a no-op. This is the property that makes the sweep safe to run
// on every ticker tick rather than once a day.
func TestSweepAuditKeepsFreshRows(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()

	for _, actor := range []string{"agent:narrator-01", "agent:narrator-02", "human:webui"} {
		f.ageAuditRow(t, f.writeAuditRow(t, actor), 1)
	}

	before, err := f.svc.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}

	archived, deleted, err := f.svc.SweepAudit(ctx, 30, 30)
	if err != nil {
		t.Fatalf("SweepAudit: %v", err)
	}
	if archived != 0 || deleted != 0 {
		t.Fatalf("SweepAudit = (%d, %d), want (0, 0) (every row is inside the window)", archived, deleted)
	}

	after, err := f.svc.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("ListAudit after sweep: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("ListAudit returned %d entries after the sweep, want %d (a no-op sweep changes nothing)",
			len(after), len(before))
	}
	if n := f.archiveRows(t); n != 0 {
		t.Errorf("archive rows = %d, want 0", n)
	}
}

// TestSweepAuditDeletesAgedArchive verifies the second window: a row that has
// already been archived is deleted once the archive window has passed, and
// the archive table is left empty.
func TestSweepAuditDeletesAgedArchive(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()

	agentOld := f.writeAuditRow(t, "agent:narrator-01")
	f.ageAuditRow(t, agentOld, 40)

	// First sweep moves the row out of the live log.
	archived, deleted, err := f.svc.SweepAudit(ctx, 30, 30)
	if err != nil {
		t.Fatalf("first SweepAudit: %v", err)
	}
	if archived != 1 || deleted != 0 {
		t.Fatalf("first SweepAudit = (%d, %d), want (1, 0)", archived, deleted)
	}
	if n := f.archiveRows(t); n != 1 {
		t.Fatalf("archive rows = %d, want 1 after the first sweep", n)
	}

	// Age the archived row past the archive window as well.
	f.ageArchivedRow(t, agentOld, 40)

	archived, deleted, err = f.svc.SweepAudit(ctx, 30, 30)
	if err != nil {
		t.Fatalf("second SweepAudit: %v", err)
	}
	if archived != 0 || deleted != 0 {
		t.Fatalf("second SweepAudit = (%d, %d), want (0, 0) (the archive is now empty)", archived, deleted)
	}
	if n := f.archiveRows(t); n != 0 {
		t.Errorf("archive rows = %d, want 0 (the archive window has passed)", n)
	}
}

// TestSweepAuditRejectsBadArgs verifies a non-positive window is refused
// rather than silently treated as "keep nothing" or "keep everything". A
// sweep that misread its configuration would delete the audit log the
// operator relies on.
func TestSweepAuditRejectsBadArgs(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()

	f.writeAuditRow(t, "agent:narrator-01")
	f.writeAuditRow(t, "human:webui")

	cases := []struct {
		name        string
		retainDays  int
		archiveDays int
	}{
		{"zero retain", 0, 30},
		{"negative retain", -1, 30},
		{"zero archive", 30, 0},
		{"negative archive", 30, -5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archived, deleted, err := f.svc.SweepAudit(ctx, tc.retainDays, tc.archiveDays)
			if !errors.Is(err, model.ErrArgument) {
				t.Fatalf("SweepAudit(%d, %d) err = %v, want model.ErrArgument",
					tc.retainDays, tc.archiveDays, err)
			}
			if archived != 0 || deleted != 0 {
				t.Errorf("a rejected SweepAudit returned (%d, %d), want (0, 0)", archived, deleted)
			}
		})
	}

	// A rejected sweep must leave both tables untouched.
	remaining, err := f.svc.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(remaining) != 2 {
		t.Errorf("ListAudit returned %d entries, want 2 (rejected sweeps delete nothing)", len(remaining))
	}
	if n := f.archiveRows(t); n != 0 {
		t.Errorf("archive rows = %d, want 0", n)
	}
}

// TestSweepAuditIsIdempotent verifies running the sweep twice in a row does
// nothing the second time. The ticker runs this on a schedule, so a second
// run in the same window must not double-count or re-touch rows.
func TestSweepAuditIsIdempotent(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()

	agentOld := f.writeAuditRow(t, "agent:narrator-01")
	systemOld := f.writeAuditRow(t, "human:webui")
	f.ageAuditRow(t, agentOld, 40)
	f.ageAuditRow(t, systemOld, 40)

	firstArchived, firstDeleted, err := f.svc.SweepAudit(ctx, 30, 30)
	if err != nil {
		t.Fatalf("first SweepAudit: %v", err)
	}
	if firstArchived != 1 || firstDeleted != 1 {
		t.Fatalf("first SweepAudit = (%d, %d), want (1, 1)", firstArchived, firstDeleted)
	}

	archived, deleted, err := f.svc.SweepAudit(ctx, 30, 30)
	if err != nil {
		t.Fatalf("second SweepAudit: %v", err)
	}
	if archived != 0 || deleted != 0 {
		t.Fatalf("second SweepAudit = (%d, %d), want (0, 0)", archived, deleted)
	}

	// The archive still holds exactly the one row, and the live log is empty.
	if n := f.archiveRows(t); n != 1 {
		t.Errorf("archive rows = %d, want 1", n)
	}
	remaining, err := f.svc.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("ListAudit returned %d entries, want 0", len(remaining))
	}
}
