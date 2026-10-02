ALTER TABLE assets ADD COLUMN input_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN ingest_run_id TEXT NOT NULL DEFAULT '';
CREATE TABLE recording_sources (
 source_id TEXT PRIMARY KEY,
 asset_id TEXT NOT NULL,
 root_id TEXT NOT NULL,
 relative_path TEXT NOT NULL,
 source_version TEXT NOT NULL,
 size_bytes INTEGER NOT NULL,
 mtime_ns INTEGER NOT NULL,
 snapshot_ref TEXT NOT NULL DEFAULT '',
 sha256 TEXT NOT NULL DEFAULT '',
 probe_version TEXT NOT NULL DEFAULT '',
 created_by TEXT NOT NULL,
 created_at DATETIME NOT NULL,
 published_at DATETIME
);
CREATE INDEX idx_recording_sources_asset ON recording_sources(asset_id, created_at);
CREATE TABLE ingest_runs (
 run_id TEXT PRIMARY KEY,
 asset_id TEXT NOT NULL,
 source_id TEXT NOT NULL REFERENCES recording_sources(source_id),
 state TEXT NOT NULL,
 stage TEXT NOT NULL,
 version INTEGER NOT NULL,
 policy_json TEXT NOT NULL,
 policy_sha256 TEXT NOT NULL,
 roots_sha256 TEXT NOT NULL,
 analysis_revision INTEGER NOT NULL DEFAULT 0,
 selection_revision INTEGER NOT NULL DEFAULT 0,
 current_task_id TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '',
 probe_json TEXT NOT NULL DEFAULT '',
 probe_sha256 TEXT NOT NULL DEFAULT '',
 segments_ref TEXT NOT NULL DEFAULT '',
 segments_sha256 TEXT NOT NULL DEFAULT '',
 files_json TEXT NOT NULL DEFAULT '{}',
 package_ref TEXT NOT NULL DEFAULT '',
 profile_id TEXT NOT NULL DEFAULT '',
 profile_revision INTEGER NOT NULL DEFAULT 0,
 profile_sha256 TEXT NOT NULL DEFAULT '',
 created_by TEXT NOT NULL,
 created_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX idx_ingest_runs_one_active ON ingest_runs(asset_id) WHERE state IN ('queued','processing','awaiting_review');
CREATE INDEX idx_ingest_runs_asset ON ingest_runs(asset_id, created_at);
CREATE TABLE ingest_plan_revisions (
 run_id TEXT NOT NULL REFERENCES ingest_runs(run_id),
 revision INTEGER NOT NULL,
 kind TEXT NOT NULL,
 schema_version INTEGER NOT NULL,
 canonical_json TEXT NOT NULL,
 sha256 TEXT NOT NULL,
 actor TEXT NOT NULL,
 created_at DATETIME NOT NULL,
 PRIMARY KEY(run_id, revision)
);
CREATE TABLE ingest_task_bindings (
 task_id TEXT PRIMARY KEY,
 run_id TEXT NOT NULL REFERENCES ingest_runs(run_id),
 stage TEXT NOT NULL,
 plan_revision INTEGER NOT NULL DEFAULT 0,
 input_sha256 TEXT NOT NULL,
 roots_sha256 TEXT NOT NULL,
 execution_id TEXT NOT NULL DEFAULT '',
 execution_seq INTEGER NOT NULL DEFAULT 0,
 attempt_of TEXT NOT NULL DEFAULT '',
 invalidated INTEGER NOT NULL DEFAULT 0,
 result_ref TEXT NOT NULL DEFAULT '',
 result_sha256 TEXT NOT NULL DEFAULT '',
 created_at DATETIME NOT NULL
);
CREATE INDEX idx_ingest_task_bindings_run ON ingest_task_bindings(run_id, created_at);
CREATE TABLE ingest_checkpoints (
 task_id TEXT NOT NULL REFERENCES ingest_task_bindings(task_id),
 sequence INTEGER NOT NULL,
 execution_id TEXT NOT NULL,
 input_sha256 TEXT NOT NULL,
 policy_sha256 TEXT NOT NULL,
 kind TEXT NOT NULL,
 item_index INTEGER NOT NULL,
 ref TEXT NOT NULL,
 sha256 TEXT NOT NULL,
 created_at DATETIME NOT NULL,
 PRIMARY KEY(task_id, sequence)
);
CREATE TABLE ingest_worker_capabilities (
 agent_id TEXT PRIMARY KEY,
 roots_sha256 TEXT NOT NULL,
 capability_json TEXT NOT NULL,
 observed_at DATETIME NOT NULL,
 expires_at DATETIME NOT NULL
);
