-- Migration 0002: audit log retention.
--
-- The retention policy keeps agent rows longer than ordinary ones. Two tables
-- are involved: audit_logs holds the live log, audit_logs_archive holds the
-- agent rows that fell out of the live window but are not yet disposable.
--
-- archived_at is deliberately separate from created_at. The archive window is
-- measured from the moment a row was archived, because a window measured from
-- created_at would expire a row in the same sweep that moved it, which would
-- make the archive table dead weight.
CREATE TABLE IF NOT EXISTS audit_logs_archive (
  id INTEGER PRIMARY KEY,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  detail TEXT,
  created_at DATETIME NOT NULL,
  archived_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_archive_archived_at ON audit_logs_archive(archived_at);
