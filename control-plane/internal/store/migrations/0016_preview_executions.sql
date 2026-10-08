-- Execution identity is separate from immutable preview input/output identity.
CREATE TABLE preview_executions (
  execution_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(task_id),
  agent_id TEXT NOT NULL,
  runtime_instance_id TEXT NOT NULL,
  generation INTEGER NOT NULL CHECK (generation > 0),
  input_sha256 TEXT NOT NULL,
  UNIQUE (task_id, generation)
);
CREATE TABLE preview_execution_requests (
  runtime_instance_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  execution_id TEXT NOT NULL REFERENCES preview_executions(execution_id),
  PRIMARY KEY (runtime_instance_id, request_id)
);
