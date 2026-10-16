-- Auto-run mode (Stage B1). Side tables only: workflow_runs and its partial
-- unique index are untouched, so a non-auto run is byte-for-byte what it was.
-- workflow_auto deliberately has no asset_id column; by-asset lookups JOIN
-- workflow_runs.asset_id.

CREATE TABLE IF NOT EXISTS workflow_auto (
  run_id TEXT PRIMARY KEY,
  auto_version INTEGER NOT NULL,
  state TEXT NOT NULL,
  halt_reason TEXT NOT NULL DEFAULT '',
  brief TEXT NOT NULL DEFAULT '',
  brief_sha256 TEXT NOT NULL DEFAULT '',
  authorized_by TEXT NOT NULL,
  authorized_at DATETIME NOT NULL,
  revoked_by TEXT NOT NULL DEFAULT '',
  revoked_at DATETIME,
  revoke_reason TEXT NOT NULL DEFAULT '',
  consent_at_revoke TEXT NOT NULL DEFAULT '',
  consent_granted_by_run INTEGER NOT NULL DEFAULT 0,
  notice_sha256 TEXT NOT NULL DEFAULT '',
  scope_json TEXT NOT NULL,
  profile_id TEXT NOT NULL,
  profile_revision INTEGER NOT NULL,
  profile_sha256 TEXT NOT NULL,
  round INTEGER NOT NULL DEFAULT 0,
  max_rounds INTEGER NOT NULL,
  total_rounds INTEGER NOT NULL DEFAULT 0,
  max_total_rounds INTEGER NOT NULL,
  actions_used INTEGER NOT NULL DEFAULT 0,
  tts_chars_used INTEGER NOT NULL DEFAULT 0,
  delivery_hold INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  deadline_at DATETIME NOT NULL,
  paused_at DATETIME,
  decision_deadline_at DATETIME
);
-- run_id is the primary key; state is indexed for the reconciler's startup
-- scan of active runs and for the by-asset guard (state filter, then JOIN).
CREATE INDEX IF NOT EXISTS idx_workflow_auto_state ON workflow_auto(state);

CREATE TABLE IF NOT EXISTS workflow_auto_rounds (
  run_id TEXT NOT NULL,
  round INTEGER NOT NULL,
  base_revision_id TEXT NOT NULL,
  feedback TEXT NOT NULL DEFAULT '',
  started_at DATETIME NOT NULL,
  ended_at DATETIME,
  outcome TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (run_id, round)
);

CREATE TABLE IF NOT EXISTS auto_reviews (
  review_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  round INTEGER NOT NULL,
  revision_id TEXT NOT NULL,
  preview_id TEXT NOT NULL DEFAULT '',
  scorer_version TEXT NOT NULL,
  score REAL NOT NULL,
  band TEXT NOT NULL,
  reasons_json TEXT NOT NULL DEFAULT '[]',
  inputs_json TEXT NOT NULL DEFAULT '{}',
  thresholds_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auto_reviews_run ON auto_reviews(run_id, created_at);

-- Idempotency ledger. A row is inserted in the same transaction as the action
-- it records, so a row exists if and only if the action committed.
CREATE TABLE IF NOT EXISTS auto_actions (
  run_id TEXT NOT NULL,
  action_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  PRIMARY KEY (run_id, action_key)
);
-- Per-stage retry counts are COUNT(*) over kind='retry' grouped by target.
CREATE INDEX IF NOT EXISTS idx_auto_actions_kind ON auto_actions(run_id, kind, target);

-- Retained governance record of every automatic approval and human decision.
-- Written in the same transaction as the approval it describes; the audit
-- sweep does not touch it.
CREATE TABLE IF NOT EXISTS auto_decisions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  round INTEGER NOT NULL DEFAULT 0,
  kind TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL,
  basis_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auto_decisions_run ON auto_decisions(run_id, id);
