CREATE TABLE profile_external_consents (
 profile_id TEXT NOT NULL, revision INTEGER NOT NULL, profile_sha256 TEXT NOT NULL,
 actor TEXT NOT NULL, granted INTEGER NOT NULL, updated_at DATETIME NOT NULL,
 PRIMARY KEY(profile_id,revision)
);
CREATE TABLE workflow_profile_bindings (
 run_id TEXT PRIMARY KEY, profile_id TEXT NOT NULL, revision INTEGER NOT NULL,
 profile_sha256 TEXT NOT NULL, canonical_json TEXT NOT NULL
);
CREATE TABLE worker_capabilities (
 agent_id TEXT NOT NULL, role TEXT NOT NULL, profile_sha256 TEXT NOT NULL,
 capability_json TEXT NOT NULL, observed_at DATETIME NOT NULL, expires_at DATETIME NOT NULL,
 PRIMARY KEY(agent_id,profile_sha256)
);
