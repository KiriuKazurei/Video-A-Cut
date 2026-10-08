CREATE TABLE subtitle_style_drafts (
 run_id TEXT NOT NULL, revision_id TEXT NOT NULL, draft_version INTEGER NOT NULL,
 document_json TEXT NOT NULL, PRIMARY KEY(run_id, revision_id)
);
