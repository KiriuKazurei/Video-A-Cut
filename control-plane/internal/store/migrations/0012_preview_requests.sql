-- Reused active tasks still need a durable binding for every transport request.
CREATE TABLE preview_requests (
  run_id TEXT NOT NULL REFERENCES workflow_runs(run_id),
  request_id TEXT NOT NULL,
  preview_id TEXT NOT NULL REFERENCES preview_jobs(preview_id),
  revision_id TEXT NOT NULL REFERENCES revisions(revision_id),
  expected_version INTEGER NOT NULL,
  preset_sha256 TEXT NOT NULL,
  PRIMARY KEY (run_id, request_id)
);
INSERT INTO preview_requests
SELECT run_id, request_id, preview_id, revision_id, expected_version, preset_sha256 FROM preview_jobs;
CREATE TRIGGER bind_preview_request AFTER INSERT ON preview_jobs BEGIN
  INSERT INTO preview_requests VALUES (NEW.run_id, NEW.request_id, NEW.preview_id, NEW.revision_id, NEW.expected_version, NEW.preset_sha256);
END;
