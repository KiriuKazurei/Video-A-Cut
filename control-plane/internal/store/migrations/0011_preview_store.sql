CREATE TABLE preview_jobs (
  preview_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL UNIQUE REFERENCES tasks(task_id),
  asset_id TEXT NOT NULL REFERENCES assets(asset_id),
  run_id TEXT NOT NULL REFERENCES workflow_runs(run_id),
  revision_id TEXT NOT NULL REFERENCES revisions(revision_id),
  request_id TEXT NOT NULL,
  expected_version INTEGER NOT NULL,
  input_json TEXT NOT NULL,
  input_sha256 TEXT NOT NULL,
  preset_sha256 TEXT NOT NULL,
  invalidated INTEGER NOT NULL DEFAULT 0 CHECK (invalidated IN (0, 1)),
  output_dir TEXT NOT NULL DEFAULT '',
  manifest_sha256 TEXT NOT NULL DEFAULT '',
  media_json TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  UNIQUE (run_id, request_id)
);
CREATE INDEX idx_preview_revision ON preview_jobs(asset_id, run_id, revision_id, created_at);
CREATE INDEX idx_preview_input ON preview_jobs(run_id, revision_id, input_sha256);
CREATE TABLE preview_artifacts (
  preview_id TEXT NOT NULL REFERENCES preview_jobs(preview_id),
  key TEXT NOT NULL,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  mime TEXT NOT NULL,
  size INTEGER NOT NULL CHECK (size >= 0),
  sha256 TEXT NOT NULL,
  PRIMARY KEY (preview_id, key),
  UNIQUE (preview_id, path)
);
CREATE TABLE preview_worker_capabilities (
  agent_id TEXT PRIMARY KEY,
  protocol_version INTEGER NOT NULL,
  ready INTEGER NOT NULL CHECK (ready IN (0, 1)),
  capability_json TEXT NOT NULL,
  observed_at DATETIME NOT NULL,
  expires_at DATETIME NOT NULL
);
