-- F1: visible-set confirmation, script staleness, and delivery invalidation.
-- Side tables only. workflow_runs and its JSON shape are unchanged.
-- Existing rows are not rewritten: agent_visible stays as stored
-- (ingested assets keep today's visibility). In-progress runs have no
-- confirmation row and are not blocked by these tables.

CREATE TABLE IF NOT EXISTS visible_set_confirmations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  set_sha TEXT NOT NULL,
  asset_ids_json TEXT NOT NULL,
  actor TEXT NOT NULL,
  script_finalize INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS scripts (
  script_id TEXT PRIMARY KEY,
  asset_id TEXT NOT NULL,
  status TEXT NOT NULL,
  visible_set_sha TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  finalized_by TEXT NOT NULL DEFAULT '',
  finalized_at DATETIME,
  stale_reason TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_scripts_asset ON scripts(asset_id, updated_at);

CREATE TABLE IF NOT EXISTS delivery_invalidations (
  asset_id TEXT PRIMARY KEY,
  reason TEXT NOT NULL,
  invalidated_at DATETIME NOT NULL
);
