package store

import (
	"context"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// agentActorPrefix is the actor marker that classifies an audit row as an
// agent record. The retention policy treats agent rows differently from
// system and operator rows: they are archived rather than deleted outright.
//
// The prefix is matched with an equality test on a fixed-length prefix
// instead of LIKE 'agent:%'. SQLite's LIKE is ASCII-case-insensitive, so it
// would also match "AGENT:bot" and "Agent:bot" — rows the operator never
// classified as agent records — and archive them, which silently changes
// who can still read the history of a governance action.
const agentActorPrefix = "agent:"

// agedAgentAuditWhere selects the agent rows that have fallen out of the live
// retention window. The copy and the prune inside ArchiveExpiredAudit bind the
// same constant, so the two statements can never disagree about scope — a row
// is only ever deleted once it is already archived by the same transaction.
const agedAgentAuditWhere = `substr(actor, 1, ?) = ? AND created_at < ?`

// archiveColumns is the column list the archive copy writes. It is spelled out
// rather than using SELECT * because the archive table adds archived_at, so a
// bare * would misalign the two table shapes the moment either schema changes.
const archiveColumns = `(id, actor, action, target, detail, created_at, archived_at)`

// DeleteAuditOlderThan removes every audit row whose created_at predates
// cutoff and reports how many rows it removed.
//
// A zero cutoff is rejected with model.ErrArgument rather than treated as
// "the beginning of time": a caller that computed its cutoff wrong — a
// missing config value, a failed time parse leaving the zero time — would
// otherwise delete the entire audit log in one statement, and the audit log
// exists precisely so that past actions remain reconstructible.
func (s *Store) DeleteAuditOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("store: delete audit older than: cutoff is required: %w", model.ErrArgument)
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: delete audit older than %s: %w", cutoff, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete audit older than %s: rows affected: %w", cutoff, err)
	}
	return n, nil
}

// ArchiveExpiredAudit moves every agent audit row older than cutoff into the
// audit_logs_archive table and reports how many rows it archived.
//
// Agent rows are moved, not deleted: a worker writing an unusual record is
// exactly the case an operator most needs to read back later, and deleting it
// outright would leave nothing to inspect. The move is a copy-then-prune pair
// inside a single transaction, so the two cannot drift apart, and the copy
// carries the original id so an archived row is still traceable to the entry
// the live log once showed.
//
// A zero cutoff is rejected for the same reason as DeleteAuditOlderThan: it
// would archive the entire table at once.
func (s *Store) ArchiveExpiredAudit(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("store: archive expired audit: cutoff is required: %w", model.ErrArgument)
	}

	// The copy and the prune run in one transaction. Without it a crash
	// between the two statements leaves a row in both tables, and the next
	// sweep would then report a second archive for a row that was already
	// moved.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: archive expired audit: begin: %w", err)
	}
	// Rollback is a no-op once Commit has succeeded, so one deferred call
	// covers both the success and the failure path.
	defer func() { _ = tx.Rollback() }()

	// OR IGNORE rather than a plain insert. An id already present in the
	// archive means an earlier sweep copied this row and then died before
	// pruning the live copy; re-inserting it would raise a UNIQUE constraint
	// and leave that orphaned row in place on every later attempt.
	//
	// The column list is spelled out rather than using SELECT * because the
	// archive table adds archived_at, so a bare * would misalign the two
	// table shapes the moment either schema changes.
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO audit_logs_archive `+archiveColumns+
		` SELECT id, actor, action, target, detail, created_at, ?
  FROM audit_logs
  WHERE `+agedAgentAuditWhere,
		time.Now().UTC(), len(agentActorPrefix), agentActorPrefix, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: archive expired audit (copy): %w", err)
	}
	archived, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: archive expired audit (copy): rows affected: %w", err)
	}

	// The prune uses the same predicate as the copy, so it can only ever
	// remove rows that are already safely archived by this transaction.
	if _, err := tx.ExecContext(ctx, `DELETE FROM audit_logs WHERE `+agedAgentAuditWhere,
		len(agentActorPrefix), agentActorPrefix, cutoff); err != nil {
		return archived, fmt.Errorf("store: archive expired audit (prune live copy): %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: archive expired audit: commit: %w", err)
	}
	return archived, nil
}

// DeleteAuditArchiveOlderThan removes every archived audit row whose
// archived_at predates cutoff and reports how many rows it removed. This is
// the second retention window: an archived row is kept for a while after it
// leaves the live log, and then expires.
//
// The comparison uses archived_at, not created_at, so the archive window
// starts when the row was moved out of the live log. Comparing created_at
// instead would make the archive window a second copy of the live one: a row
// that has just crossed the live cutoff is necessarily already older than the
// archive cutoff whenever the two windows are equal, so it would be deleted
// by the very sweep that archived it and no row would ever survive to be
// read. archived_at is what makes the ~60 day total lifetime an operator can
// actually reason about: N days in the live log, then N days in the archive.
//
// A zero cutoff is rejected for the same reason as the other two queries.
func (s *Store) DeleteAuditArchiveOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("store: delete audit archive older than: cutoff is required: %w", model.ErrArgument)
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_logs_archive WHERE archived_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: delete audit archive older than %s: %w", cutoff, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete audit archive older than %s: rows affected: %w", cutoff, err)
	}
	return n, nil
}
