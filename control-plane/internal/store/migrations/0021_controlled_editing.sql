CREATE TABLE vac_agent_journal (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  body TEXT NOT NULL,
  previous_sha TEXT NOT NULL,
  sha TEXT NOT NULL UNIQUE
);
CREATE TRIGGER vac_agent_journal_no_update BEFORE UPDATE ON vac_agent_journal
BEGIN SELECT RAISE(ABORT, 'Agent journal is append only'); END;
CREATE TRIGGER vac_agent_journal_no_delete BEFORE DELETE ON vac_agent_journal
BEGIN SELECT RAISE(ABORT, 'Agent journal is append only'); END;
CREATE TABLE vac_edit_checkpoints (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  parent_id TEXT NOT NULL,
  body TEXT NOT NULL,
  sha TEXT NOT NULL
);
CREATE INDEX vac_edit_checkpoints_run ON vac_edit_checkpoints(run_id);
CREATE TRIGGER vac_edit_checkpoints_no_update BEFORE UPDATE ON vac_edit_checkpoints
BEGIN SELECT RAISE(ABORT, 'Edit checkpoints are immutable'); END;
CREATE TRIGGER vac_edit_checkpoints_no_delete BEFORE DELETE ON vac_edit_checkpoints
BEGIN SELECT RAISE(ABORT, 'Edit checkpoints are immutable'); END;
CREATE TABLE vac_edit_cursors (run_id TEXT PRIMARY KEY, checkpoint_id TEXT NOT NULL, redo TEXT NOT NULL);
