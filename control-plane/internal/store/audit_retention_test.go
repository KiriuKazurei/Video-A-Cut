package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// ageAuditRow rewrites one audit row's created_at to an instant daysInPast
// days ago. WriteAudit stamps its own timestamp and takes none from the
// caller, so this is the only way a test can produce a row the retention
// window has already passed.
func ageAuditRow(t *testing.T, s *store.Store, id int64, daysInPast int) {
	t.Helper()
	when := time.Now().UTC().AddDate(0, 0, -daysInPast)
	res, err := s.DB().Exec(`UPDATE audit_logs SET created_at = ? WHERE id = ?`, when, id)
	if err != nil {
		t.Fatalf("age audit row %d: %v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		t.Fatalf("age audit row %d: rows affected: %v", id, err)
	} else if n != 1 {
		t.Fatalf("age audit row %d: affected %d rows, want 1", id, n)
	}
}

// countArchiveRows reports how many rows currently sit in the archive table.
func countArchiveRows(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM audit_logs_archive`).Scan(&n); err != nil {
		t.Fatalf("count archive rows: %v", err)
	}
	return n
}

// ageArchivedRow backdates the archived_at timestamp of an already-archived
// row. The archive window is measured from archived_at, so this is how a test
// produces a row that has spent its archive window in the archive table.
func ageArchivedRow(t *testing.T, s *store.Store, id int64, daysInPast int) {
	t.Helper()
	res, err := s.DB().Exec(`UPDATE audit_logs_archive SET archived_at = ? WHERE id = ?`,
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

// TestOpenAppliesAuditArchiveMigration verifies migration 0002 creates the
// archive table the retention queries write into, and that it is recorded in
// the ledger alongside 0001.
//
// It also pins that reopening an already-migrated database does not re-run
// 0001 or 0002 and double-record them: the ledger grows by zero on the second
// open. The neighbouring TestOpenIsIdempotent makes the same point for a
// database that only ever saw 0001.
func TestOpenAppliesAuditArchiveMigration(t *testing.T) {
	s := mustOpen(t)

	for _, table := range []string{"assets", "tasks", "agents", "audit_logs", "audit_logs_archive"} {
		var name string
		err := s.DB().QueryRow(`select name from sqlite_master where type='table' and name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing from sqlite_master: %v", table, err)
		}
		if name != table {
			t.Fatalf("table %s: got %q", table, name)
		}
	}

	var versions int
	if err := s.DB().QueryRow(`select count(*) from schema_migrations`).Scan(&versions); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if versions != 2 {
		t.Fatalf("schema_migrations rows = %d, want 2", versions)
	}

	// Reopen the same file: both versions are already recorded, so the ledger
	// must not grow. The point of the ledger is exactly this.
	dbPath := t.TempDir() + "/reopen.db"
	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	var afterOpen int
	if err := s2.DB().QueryRow(`select count(*) from schema_migrations`).Scan(&afterOpen); err != nil {
		t.Fatalf("count after first open: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s3, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = s3.Close() })
	var afterReopen int
	if err := s3.DB().QueryRow(`select count(*) from schema_migrations`).Scan(&afterReopen); err != nil {
		t.Fatalf("count after reopen: %v", err)
	}
	if afterReopen != afterOpen {
		t.Fatalf("schema_migrations rows went from %d to %d on reopen, want unchanged (migrations must not re-apply)",
			afterOpen, afterReopen)
	}
}

// TestDeleteAuditOlderThanRemovesOnlyAgedRows pins the plain retention rule:
// every row older than the cutoff goes, everything inside the window stays.
// A non-positive cutoff must delete nothing rather than everything.
func TestDeleteAuditOlderThanRemovesOnlyAgedRows(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	seed := func(actor string, days int) int64 {
		t.Helper()
		if err := s.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "asset.approve", Target: "clip_001"}); err != nil {
			t.Fatalf("WriteAudit(%s): %v", actor, err)
		}
		var id int64
		if err := s.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
			t.Fatalf("read back id: %v", err)
		}
		ageAuditRow(t, s, id, days)
		return id
	}

	oldID := seed("human:webui", 40)
	freshID := seed("human:webui", 1)

	cutoff := time.Now().UTC().AddDate(0, 0, -30)
	deleted, err := s.DeleteAuditOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteAuditOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the 40-day-old row)", deleted)
	}

	// The aged-out row is gone and the fresh one survives, which is what makes
	// the rule "purge old records" rather than "purge records".
	logs, err := s.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListAudit returned %d entries, want 1: %+v", len(logs), logs)
	}
	if logs[0].ID != freshID {
		t.Errorf("surviving audit row id = %d, want %d", logs[0].ID, freshID)
	}
	if logs[0].ID == oldID {
		t.Errorf("audit row %d should have been deleted", oldID)
	}

	// Running the same sweep again must be a no-op: the row it wanted is
	// already gone.
	again, err := s.DeleteAuditOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("repeat DeleteAuditOlderThan: %v", err)
	}
	if again != 0 {
		t.Errorf("repeat sweep deleted = %d, want 0", again)
	}
}

// TestDeleteAuditOlderThanRejectsNonPositiveCutoff verifies a zero cutoff
// deletes nothing instead of wiping the table. The store layer refuses it
// rather than treating a caller error as "delete everything".
func TestDeleteAuditOlderThanRejectsNonPositiveCutoff(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.WriteAudit(ctx, model.AuditLog{Actor: "human:webui", Action: "asset.approve", Target: "clip_001"}); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	_, err := s.DeleteAuditOlderThan(ctx, time.Time{})
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("DeleteAuditOlderThan with zero cutoff: err = %v, want model.ErrArgument", err)
	}

	logs, err := s.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("ListAudit returned %d entries, want 1 (a rejected sweep must delete nothing)", len(logs))
	}
}

// TestArchiveExpiredAuditMovesAgentRows verifies the archive rule: rows whose
// actor starts with "agent:" and that predate the cutoff are MOVED into the
// archive table (copied, then deleted from the live log) while every other
// row is left in place, whatever its age.
func TestArchiveExpiredAuditMovesAgentRows(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	seed := func(actor string, days int) int64 {
		t.Helper()
		if err := s.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "task.claim", Target: "t_001", Detail: "{}"}); err != nil {
			t.Fatalf("WriteAudit(%s): %v", actor, err)
		}
		var id int64
		if err := s.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
			t.Fatalf("read back id: %v", err)
		}
		ageAuditRow(t, s, id, days)
		return id
	}

	// Two aged agent rows must move, and neither an aged system row nor a
	// fresh agent row may.
	agedAgent1 := seed("agent:narrator-01", 40)
	agedAgent2 := seed("agent:narrator-02", 45)
	agedSystem := seed("human:webui", 40)
	freshAgent := seed("agent:narrator-01", 1)

	cutoff := time.Now().UTC().AddDate(0, 0, -30)
	archived, err := s.ArchiveExpiredAudit(ctx, cutoff)
	if err != nil {
		t.Fatalf("ArchiveExpiredAudit: %v", err)
	}
	if archived != 2 {
		t.Fatalf("archived = %d, want 2 (the two 30+ day old agent rows)", archived)
	}

	logs, err := s.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("ListAudit returned %d entries, want 2 (aged system + fresh agent): %+v", len(logs), logs)
	}
	for _, l := range logs {
		if l.ID == agedAgent1 || l.ID == agedAgent2 {
			t.Errorf("aged agent row %d must not remain in the live log", l.ID)
		}
		if l.ID != agedSystem && l.ID != freshAgent {
			t.Errorf("unexpected surviving row %d (%s)", l.ID, l.Actor)
		}
	}

	// Both moved rows are physically present in the archive table, with their
	// original id preserved so an operator can still trace them.
	var got int
	if err := s.DB().QueryRow(`select count(*) from audit_logs_archive where id in (?, ?)`,
		agedAgent1, agedAgent2).Scan(&got); err != nil {
		t.Fatalf("count archive rows: %v", err)
	}
	if got != 2 {
		t.Errorf("archive rows for the moved ids = %d, want 2", got)
	}

	var actor string
	if err := s.DB().QueryRow(`select actor from audit_logs_archive where id = ?`, agedAgent1).Scan(&actor); err != nil {
		t.Fatalf("read archived actor: %v", err)
	}
	if actor != "agent:narrator-01" {
		t.Errorf("archived actor = %q, want %q", actor, "agent:narrator-01")
	}

	// The move is one-shot: a second archive sweep finds nothing left to move.
	again, err := s.ArchiveExpiredAudit(ctx, cutoff)
	if err != nil {
		t.Fatalf("repeat ArchiveExpiredAudit: %v", err)
	}
	if again != 0 {
		t.Errorf("repeat archive sweep = %d, want 0", again)
	}
	if n := countArchiveRows(t, s); n != 2 {
		t.Errorf("archive rows after repeat sweep = %d, want 2 (no duplicates)", n)
	}
}

// TestArchiveExpiredAuditIsCaseSensitiveOnPrefix verifies the prefix rule is
// a literal "agent:" match. SQLite's LIKE is ASCII-case-insensitive and would
// otherwise archive an actor such as "AGENT:bot" that the operator never
// classified as an agent record.
func TestArchiveExpiredAuditIsCaseSensitiveOnPrefix(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	seed := func(actor string) int64 {
		t.Helper()
		if err := s.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "task.claim", Target: "t_001"}); err != nil {
			t.Fatalf("WriteAudit(%s): %v", actor, err)
		}
		var id int64
		if err := s.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
			t.Fatalf("read back id: %v", id)
		}
		ageAuditRow(t, s, id, 40)
		return id
	}

	lower := seed("agent:narrator-01")
	upper := seed("AGENT:narrator-01")
	mixed := seed("Agent:narrator-01")

	cutoff := time.Now().UTC().AddDate(0, 0, -30)
	archived, err := s.ArchiveExpiredAudit(ctx, cutoff)
	if err != nil {
		t.Fatalf("ArchiveExpiredAudit: %v", err)
	}
	if archived != 1 {
		t.Fatalf("archived = %d, want 1 (only the lowercase agent: prefix)", archived)
	}

	var moved int64 = -1
	if err := s.DB().QueryRow(`select id from audit_logs_archive`).Scan(&moved); err != nil {
		t.Fatalf("read archived id: %v", err)
	}
	if moved != lower {
		t.Errorf("archived id = %d, want %d", moved, lower)
	}

	for _, id := range []int64{upper, mixed} {
		var stillLive bool
		if err := s.DB().QueryRow(`select exists(select 1 from audit_logs where id = ?)`, id).Scan(&stillLive); err != nil {
			t.Fatalf("check live row %d: %v", id, err)
		}
		if !stillLive {
			t.Errorf("row %d was archived but its actor is not a literal agent: prefix", id)
		}
	}
}

// TestArchiveExpiredAuditRejectsNonPositiveCutoff verifies the guard: a zero
// cutoff must not archive the entire table.
func TestArchiveExpiredAuditRejectsNonPositiveCutoff(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.WriteAudit(ctx, model.AuditLog{Actor: "agent:narrator-01", Action: "task.claim", Target: "t_001"}); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	_, err := s.ArchiveExpiredAudit(ctx, time.Time{})
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("ArchiveExpiredAudit with zero cutoff: err = %v, want model.ErrArgument", err)
	}
	if n := countArchiveRows(t, s); n != 0 {
		t.Errorf("archive rows = %d, want 0 (a rejected sweep must archive nothing)", n)
	}
}

// TestDeleteAuditArchiveOlderThan verifies the second retention window: an
// archived row is deleted once it has itself spent the archive window in the
// archive table, and rows that have been there for less are kept.
func TestDeleteAuditArchiveOlderThan(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	// Archive two agent rows through the real archive path so the archive
	// table holds rows with the shape the retention query reads.
	seed := func(actor string, days int) int64 {
		t.Helper()
		if err := s.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "task.claim", Target: "t_001"}); err != nil {
			t.Fatalf("WriteAudit(%s): %v", actor, err)
		}
		var id int64
		if err := s.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
			t.Fatalf("read back id: %v", err)
		}
		ageAuditRow(t, s, id, days)
		return id
	}

	// Both rows are past the 30-day live window, so both move out.
	recentArchived := seed("agent:narrator-02", 31)
	oldArchived := seed("agent:narrator-01", 45)

	liveCutoff := time.Now().UTC().AddDate(0, 0, -30)
	archived, err := s.ArchiveExpiredAudit(ctx, liveCutoff)
	if err != nil {
		t.Fatalf("ArchiveExpiredAudit: %v", err)
	}
	if archived != 2 {
		t.Fatalf("archived = %d, want 2 (both agent rows are past the live window)", archived)
	}
	if n := countArchiveRows(t, s); n != 2 {
		t.Fatalf("archive rows after the archive sweep = %d, want 2", n)
	}

	// A row must survive at least its own archive window after being moved,
	// which is the point of archiving rather than deleting it outright. With
	// equal windows this is what makes the total lifetime about twice the
	// live one.
	ageArchivedRow(t, s, oldArchived, 40)
	ageArchivedRow(t, s, recentArchived, 5)

	archiveCutoff := time.Now().UTC().AddDate(0, 0, -30)
	deleted, err := s.DeleteAuditArchiveOlderThan(ctx, archiveCutoff)
	if err != nil {
		t.Fatalf("DeleteAuditArchiveOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the row that has sat archived past the archive window)", deleted)
	}
	if n := countArchiveRows(t, s); n != 1 {
		t.Errorf("archive rows = %d, want 1 (the recently archived row remains)", n)
	}

	var survivor int64 = -1
	if err := s.DB().QueryRow(`select id from audit_logs_archive`).Scan(&survivor); err != nil {
		t.Fatalf("read surviving archive row: %v", err)
	}
	if survivor != recentArchived {
		t.Errorf("surviving archive row id = %d, want %d", survivor, recentArchived)
	}

	// Idempotent: the row it wanted is gone.
	again, err := s.DeleteAuditArchiveOlderThan(ctx, archiveCutoff)
	if err != nil {
		t.Fatalf("repeat DeleteAuditArchiveOlderThan: %v", err)
	}
	if again != 0 {
		t.Errorf("repeat sweep deleted = %d, want 0", again)
	}
}

// TestArchiveRetentionGivesAgentRowsDoubleLifetime pins the whole point of the
// retention policy, end to end, with the default 30/30 windows: an agent row
// survives about twice as long as an ordinary row. With windows measured the
// same way off created_at these two rows would expire together, and the
// archive table would be pointless.
func TestArchiveRetentionGivesAgentRowsDoubleLifetime(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	seedAndAge := func(actor string, days int) {
		t.Helper()
		if err := s.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "task.claim", Target: "t_001"}); err != nil {
			t.Fatalf("WriteAudit(%s): %v", actor, err)
		}
		var id int64
		if err := s.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
			t.Fatalf("read back id: %v", err)
		}
		ageAuditRow(t, s, id, days)
	}

	// Both rows are 35 days old: past the 30-day live window, inside the
	// archive window that starts when the agent row is moved.
	seedAndAge("agent:narrator-01", 35)
	seedAndAge("human:webui", 35)

	// First sweep: the agent row moves, the ordinary row is deleted.
	retainCutoff := time.Now().UTC().AddDate(0, 0, -30)
	archived, err := s.ArchiveExpiredAudit(ctx, retainCutoff)
	if err != nil {
		t.Fatalf("ArchiveExpiredAudit: %v", err)
	}
	if archived != 1 {
		t.Fatalf("archived = %d, want 1", archived)
	}
	deleted, err := s.DeleteAuditOlderThan(ctx, retainCutoff)
	if err != nil {
		t.Fatalf("DeleteAuditOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (the ordinary row)", deleted)
	}

	// A second sweep one instant later must keep the archived row: it was
	// moved seconds ago, so its own 30-day archive window has barely started.
	archived, err = s.ArchiveExpiredAudit(ctx, retainCutoff)
	if err != nil {
		t.Fatalf("second ArchiveExpiredAudit: %v", err)
	}
	if archived != 0 {
		t.Errorf("second sweep archived = %d, want 0 (nothing left to move)", archived)
	}
	archiveDeleted, err := s.DeleteAuditArchiveOlderThan(ctx, time.Now().UTC().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("DeleteAuditArchiveOlderThan: %v", err)
	}
	if archiveDeleted != 0 {
		t.Errorf("archive sweep deleted = %d, want 0 (a row just archived is not immediately expired)", archiveDeleted)
	}
	if n := countArchiveRows(t, s); n != 1 {
		t.Errorf("archive rows = %d, want 1 (still readable)", n)
	}

	// Only after the archive row has itself sat in the archive for the full
	// archive window does it go. Total lifetime is therefore 30 days in the
	// live log plus 30 days in the archive, not 30 days in total.
	ageArchivedRow(t, s, 1, 31)
	archiveDeleted, err = s.DeleteAuditArchiveOlderThan(ctx, time.Now().UTC().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("final DeleteAuditArchiveOlderThan: %v", err)
	}
	if archiveDeleted != 1 {
		t.Fatalf("final archive sweep deleted = %d, want 1 (the archive window has passed)", archiveDeleted)
	}
	if n := countArchiveRows(t, s); n != 0 {
		t.Errorf("archive rows = %d, want 0", n)
	}
}

// TestArchiveExpiredAuditRecoversOrphanedLiveRow covers the crash between the
// copy and the prune: if the process dies after a row reached the archive but
// before its live copy was deleted, the next sweep must finish the job instead
// of failing forever on the duplicate id. The INSERT OR IGNORE is what makes
// that possible; a plain INSERT would raise a UNIQUE constraint and leave the
// row stuck in both tables on every later attempt.
func TestArchiveExpiredAuditRecoversOrphanedLiveRow(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.WriteAudit(ctx, model.AuditLog{Actor: "agent:narrator-01", Action: "task.claim", Target: "t_001"}); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}
	var id int64
	if err := s.DB().QueryRow(`select id from audit_logs where rowid = last_insert_rowid()`).Scan(&id); err != nil {
		t.Fatalf("read back id: %v", err)
	}
	ageAuditRow(t, s, id, 40)

	// Replay exactly what an interrupted sweep left behind: the archive copy
	// committed, the live row not yet deleted.
	if _, err := s.DB().Exec(`INSERT INTO audit_logs_archive
  (id, actor, action, target, detail, created_at, archived_at)
  SELECT id, actor, action, target, detail, created_at, ? FROM audit_logs WHERE id = ?`,
		time.Now().UTC().AddDate(0, 0, -5), id); err != nil {
		t.Fatalf("simulate interrupted sweep: %v", err)
	}
	var liveBefore int
	if err := s.DB().QueryRow(`select count(*) from audit_logs`).Scan(&liveBefore); err != nil {
		t.Fatalf("count live: %v", err)
	}
	if liveBefore != 1 {
		t.Fatalf("live rows = %d, want 1 (the orphan left by the interrupted sweep)", liveBefore)
	}

	// The next sweep must not error on the duplicate id, and must clean up.
	if _, err := s.ArchiveExpiredAudit(ctx, time.Now().UTC().AddDate(0, 0, -30)); err != nil {
		t.Fatalf("ArchiveExpiredAudit after an interrupted sweep: %v", err)
	}

	var liveAfter int
	if err := s.DB().QueryRow(`select count(*) from audit_logs`).Scan(&liveAfter); err != nil {
		t.Fatalf("count live after: %v", err)
	}
	if liveAfter != 0 {
		t.Errorf("live rows = %d, want 0 (the orphan was pruned)", liveAfter)
	}
	if n := countArchiveRows(t, s); n != 1 {
		t.Errorf("archive rows = %d, want 1 (exactly one copy, not a duplicate)", n)
	}
}

// TestDeleteAuditArchiveOlderThanRejectsNonPositiveCutoff verifies a zero
// cutoff clears nothing from the archive.
func TestDeleteAuditArchiveOlderThanRejectsNonPositiveCutoff(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.WriteAudit(ctx, model.AuditLog{Actor: "agent:narrator-01", Action: "task.claim", Target: "t_001"}); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}
	if _, err := s.ArchiveExpiredAudit(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("ArchiveExpiredAudit: %v", err)
	}
	if n := countArchiveRows(t, s); n != 1 {
		t.Fatalf("archive rows = %d, want 1 before the rejected sweep", n)
	}

	_, err := s.DeleteAuditArchiveOlderThan(ctx, time.Time{})
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("DeleteAuditArchiveOlderThan with zero cutoff: err = %v, want model.ErrArgument", err)
	}
	if n := countArchiveRows(t, s); n != 1 {
		t.Errorf("archive rows = %d, want 1 (a rejected sweep must delete nothing)", n)
	}
}
