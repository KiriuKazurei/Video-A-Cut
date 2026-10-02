CREATE TABLE IF NOT EXISTS revisions (
  revision_id TEXT PRIMARY KEY,
  asset_id TEXT NOT NULL,
  parent_revision_id TEXT,
  schema_version INTEGER NOT NULL,
  package_ref TEXT NOT NULL,
  edl_sha256 TEXT NOT NULL,
  evidence_manifest_sha256 TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  created_by TEXT NOT NULL,
  reason TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_revisions_asset ON revisions(asset_id, created_at);

CREATE TABLE IF NOT EXISTS workflow_runs (
  run_id TEXT PRIMARY KEY,
  asset_id TEXT NOT NULL,
  base_revision_id TEXT NOT NULL,
  current_revision_id TEXT NOT NULL,
  status TEXT NOT NULL,
  stage TEXT NOT NULL,
  version INTEGER NOT NULL,
  content_mode TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  blocked_reason TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_workflow_one_active
  ON workflow_runs(asset_id)
  WHERE status IN ('created', 'running', 'awaiting_review', 'failed');

CREATE TABLE IF NOT EXISTS workflow_stages (
  run_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  stage TEXT NOT NULL,
  input_revision_id TEXT NOT NULL,
  output_revision_id TEXT NOT NULL DEFAULT '',
  attempt_of TEXT NOT NULL DEFAULT '',
  invalidated INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (run_id, task_id)
);
CREATE INDEX IF NOT EXISTS idx_workflow_stages_task ON workflow_stages(task_id);

CREATE TABLE IF NOT EXISTS scene_reviews (
  run_id TEXT NOT NULL,
  revision_id TEXT NOT NULL,
  scene_id TEXT NOT NULL,
  decision TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  evidence_sha256 TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (run_id, revision_id, scene_id)
);

CREATE TABLE IF NOT EXISTS workflow_narration_approvals (
  run_id TEXT NOT NULL,
  revision_id TEXT NOT NULL,
  narration_id TEXT NOT NULL,
  draft_hash TEXT NOT NULL,
  actor TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (run_id, revision_id, narration_id)
);

CREATE TABLE IF NOT EXISTS acceptance_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  export_revision_id TEXT NOT NULL,
  manifest_sha256 TEXT NOT NULL,
  check_item TEXT NOT NULL,
  result TEXT NOT NULL,
  actor TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_acceptance_run ON acceptance_records(run_id, id);

CREATE TABLE IF NOT EXISTS idempotency_keys (
  scope TEXT NOT NULL,
  idem_key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_json TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (scope, idem_key)
);
