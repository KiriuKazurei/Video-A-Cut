ALTER TABLE ingest_task_bindings ADD COLUMN handover_state TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest_task_bindings ADD COLUMN execution_state TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest_checkpoints ADD COLUMN manifest_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE ingest_checkpoints ADD COLUMN journal_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ingest_checkpoints ADD COLUMN source_execution_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest_checkpoints ADD COLUMN item_status TEXT NOT NULL DEFAULT 'registered';
ALTER TABLE ingest_checkpoints ADD COLUMN summary TEXT NOT NULL DEFAULT '';

CREATE TABLE ingest_executions (
 execution_id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL,
 run_id TEXT NOT NULL,
 generation INTEGER NOT NULL,
 agent_id TEXT NOT NULL,
 role TEXT NOT NULL,
 runtime_instance_id TEXT NOT NULL,
 input_sha256 TEXT NOT NULL,
 policy_sha256 TEXT NOT NULL,
 status TEXT NOT NULL CHECK (status IN (
  'allocated','waiting_resource','running','suspended_connection',
  'stop_requested','draining','stopped','succeeded','failed','cleanup_blocked')),
 lease_until DATETIME,
 owned_dir TEXT NOT NULL,
 stop_reason TEXT NOT NULL DEFAULT '',
 drained_at DATETIME,
 result_request_id TEXT NOT NULL DEFAULT '',
 submitted_at DATETIME,
 created_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 UNIQUE(task_id, generation)
);
CREATE INDEX idx_ingest_executions_agent ON ingest_executions(agent_id, status);
CREATE INDEX idx_ingest_executions_run ON ingest_executions(run_id, created_at);

CREATE TABLE ingest_worker_instances (
 agent_id TEXT NOT NULL,
 role TEXT NOT NULL,
 runtime_instance_id TEXT NOT NULL,
 status TEXT NOT NULL,
 observed_at DATETIME NOT NULL,
 PRIMARY KEY(agent_id, runtime_instance_id)
);

CREATE TABLE execution_controls (
 execution_id TEXT PRIMARY KEY,
 control_version INTEGER NOT NULL,
 command TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL DEFAULT '',
 acked_at DATETIME,
 ack_outcome TEXT NOT NULL DEFAULT '',
 updated_at DATETIME NOT NULL
);

CREATE TABLE execution_resources (
 resource_key TEXT PRIMARY KEY,
 owner_execution_id TEXT NOT NULL DEFAULT '',
 owner_generation INTEGER NOT NULL DEFAULT 0,
 strategy TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 barrier INTEGER NOT NULL DEFAULT 0,
 updated_at DATETIME NOT NULL
);

CREATE TABLE execution_requests (
 runtime_instance_id TEXT NOT NULL,
 request_id TEXT NOT NULL,
 operation TEXT NOT NULL,
 agent_id TEXT NOT NULL,
 execution_id TEXT NOT NULL,
 content_sha256 TEXT NOT NULL DEFAULT '',
 result_status TEXT NOT NULL DEFAULT '',
 created_at DATETIME NOT NULL,
 PRIMARY KEY(runtime_instance_id, request_id, operation)
);
