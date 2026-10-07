CREATE TABLE delivery_variants (
  variant_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL UNIQUE REFERENCES tasks(task_id),
  asset_id TEXT NOT NULL REFERENCES assets(asset_id),
  run_id TEXT NOT NULL REFERENCES workflow_runs(run_id),
  revision_id TEXT NOT NULL REFERENCES revisions(revision_id),
  request_id TEXT NOT NULL,
  expected_version INTEGER NOT NULL,
  input_json TEXT NOT NULL,
  input_sha256 TEXT NOT NULL,
  spec_sha256 TEXT NOT NULL,
  invalidated INTEGER NOT NULL DEFAULT 0 CHECK (invalidated IN (0, 1)),
  output_dir TEXT NOT NULL DEFAULT '',
  manifest_sha256 TEXT NOT NULL DEFAULT '',
  report_json TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  UNIQUE (run_id, request_id)
);
CREATE INDEX idx_delivery_variant_revision ON delivery_variants(asset_id, run_id, revision_id, created_at);
CREATE INDEX idx_delivery_variant_input ON delivery_variants(run_id, revision_id, input_sha256);
CREATE TABLE delivery_variant_artifacts (
  variant_id TEXT NOT NULL REFERENCES delivery_variants(variant_id),
  key TEXT NOT NULL,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  mime TEXT NOT NULL,
  size INTEGER NOT NULL CHECK (size >= 0),
  sha256 TEXT NOT NULL,
  PRIMARY KEY (variant_id, key),
  UNIQUE (variant_id, path)
);
CREATE TABLE delivery_variant_worker_capabilities (
  agent_id TEXT PRIMARY KEY,
  protocol_version INTEGER NOT NULL,
  ready INTEGER NOT NULL CHECK (ready IN (0, 1)),
  capability_json TEXT NOT NULL,
  observed_at DATETIME NOT NULL,
  expires_at DATETIME NOT NULL
);
-- Reused active tasks still need a durable binding for every transport request.
CREATE TABLE delivery_variant_requests (
  run_id TEXT NOT NULL REFERENCES workflow_runs(run_id),
  request_id TEXT NOT NULL,
  variant_id TEXT NOT NULL REFERENCES delivery_variants(variant_id),
  revision_id TEXT NOT NULL REFERENCES revisions(revision_id),
  expected_version INTEGER NOT NULL,
  spec_sha256 TEXT NOT NULL,
  PRIMARY KEY (run_id, request_id)
);
INSERT INTO delivery_variant_requests
SELECT run_id, request_id, variant_id, revision_id, expected_version, spec_sha256 FROM delivery_variants;
CREATE TRIGGER bind_delivery_variant_request AFTER INSERT ON delivery_variants BEGIN
  INSERT INTO delivery_variant_requests VALUES (NEW.run_id, NEW.request_id, NEW.variant_id, NEW.revision_id, NEW.expected_version, NEW.spec_sha256);
END;
