CREATE TABLE IF NOT EXISTS processing_profiles (
  profile_id TEXT PRIMARY KEY,
  current_revision INTEGER NOT NULL,
  archived INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS processing_profile_revisions (
  profile_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  canonical_json TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  actor TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (profile_id, revision)
);
