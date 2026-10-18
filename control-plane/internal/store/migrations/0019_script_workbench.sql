-- F2 keeps the F1 scripts row and workflow_runs JSON intact.
CREATE TABLE script_workbenches (
  script_id TEXT PRIMARY KEY REFERENCES scripts(script_id),
  version INTEGER NOT NULL,
  document_json TEXT NOT NULL
);
CREATE TABLE script_revisions (
  script_id TEXT NOT NULL REFERENCES scripts(script_id),
  version INTEGER NOT NULL,
  action TEXT NOT NULL,
  actor TEXT NOT NULL,
  snapshot_json TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (script_id, version)
);
CREATE TABLE script_deliveries (
  script_id TEXT NOT NULL REFERENCES scripts(script_id),
  version INTEGER NOT NULL,
  visible_set_sha TEXT NOT NULL,
  package_ref TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (script_id, version)
);
