package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// SweepAudit applies the retention policy the operator configured. Rows older
// than the retention window that belong to an agent are moved into the
// archive table and then deleted from the live log; the remaining aged-out
// rows are deleted outright; and archived rows that have themselves spent the
// archive window in the archive are deleted. Nothing that is still inside its
// window is touched, so a freshly written entry always survives a sweep.
//
// The order of the three steps is the whole point of the method, and it is the
// one thing here that is easy to get wrong. Archiving must happen before the
// plain delete: both are driven by the same cutoff, so deleting first would
// destroy the rows that were supposed to be archived and leave the archive
// permanently empty. The archive cleanup must come last, so that a row is
// archived before the archive window is consulted against it.
//
// The counts returned are the rows each step actually wrote, which is what a
// caller needs to decide whether the sweep did anything worth logging.
func (s *Service) SweepAudit(ctx context.Context, retainDays, archiveDays int) (archived, deleted int, err error) {
	if retainDays <= 0 || archiveDays <= 0 {
		return 0, 0, fmt.Errorf("service: sweep audit: retention window must be > 0 (retain=%d, archive=%d): %w",
			retainDays, archiveDays, model.ErrArgument)
	}

	utcNow := time.Now().UTC()
	retainCutoff := utcNow.AddDate(0, 0, -retainDays)
	archiveCutoff := utcNow.AddDate(0, 0, -archiveDays)

	moved, err := s.st.ArchiveExpiredAudit(ctx, retainCutoff)
	if err != nil {
		return int(moved), 0, fmt.Errorf("service: sweep audit: %w", err)
	}

	// This runs after the archive step, and its predicate therefore only ever
	// sees rows that are not agent rows: every agent row past the cutoff was
	// already moved.
	removed, err := s.st.DeleteAuditOlderThan(ctx, retainCutoff)
	if err != nil {
		return int(moved), int(removed), fmt.Errorf("service: sweep audit: %w", err)
	}

	agedOut, err := s.st.DeleteAuditArchiveOlderThan(ctx, archiveCutoff)
	if err != nil {
		return int(moved), int(removed), fmt.Errorf("service: sweep audit: %w", err)
	}

	// agedOut is reported to the log rather than returned: an operator must
	// be able to tell "rows left the live log" from "rows still in the
	// archive expired", which folding them together would blur. The sweep
	// has already done its job at this point, so the number is informational.
	slog.Default().Info("audit archive cleanup",
		"aged_out", agedOut, "retain_days", retainDays, "archive_days", archiveDays)

	return int(moved), int(removed), nil
}
