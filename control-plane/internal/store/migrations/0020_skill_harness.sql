-- New scope does not change F1/global confirmation or legacy workflow JSON.
CREATE TABLE vac_projects (
 id TEXT PRIMARY KEY, version INTEGER NOT NULL, body TEXT NOT NULL
);
CREATE TABLE vac_skills (
 id TEXT PRIMARY KEY, version INTEGER NOT NULL, body TEXT NOT NULL
);
CREATE TABLE vac_skill_runs (
 id TEXT PRIMARY KEY, version INTEGER NOT NULL, body TEXT NOT NULL
);
CREATE TABLE vac_skill_steps (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES vac_skill_runs(id),
 body TEXT NOT NULL, UNIQUE(run_id, id)
);
CREATE TABLE vac_skill_snapshots (
 run_id TEXT PRIMARY KEY REFERENCES vac_skill_runs(id), body TEXT NOT NULL
);
CREATE TABLE vac_skill_requests (
 scope TEXT NOT NULL, request_key TEXT NOT NULL, request_sha TEXT NOT NULL,
 result_id TEXT NOT NULL, PRIMARY KEY(scope, request_key)
);
CREATE TABLE vac_skill_revisions (
 run_id TEXT NOT NULL REFERENCES vac_skill_runs(id),
 run_version INTEGER NOT NULL, body TEXT NOT NULL,
 PRIMARY KEY(run_id,run_version)
);
