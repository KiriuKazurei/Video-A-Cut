CREATE TABLE tts_templates (
 template_id TEXT PRIMARY KEY, current_revision INTEGER NOT NULL,
 archived INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE tts_template_revisions (
 template_id TEXT NOT NULL, revision INTEGER NOT NULL, document_json TEXT NOT NULL,
 PRIMARY KEY(template_id, revision),
 FOREIGN KEY(template_id) REFERENCES tts_templates(template_id)
);
CREATE TABLE tts_preview_jobs (
 job_id TEXT PRIMARY KEY, document_json TEXT NOT NULL
);
