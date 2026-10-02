CREATE TABLE IF NOT EXISTS narration_reviews (
  asset_id TEXT NOT NULL,
  draft_hash TEXT NOT NULL,
  text TEXT NOT NULL,
  start_sec REAL NOT NULL,
  end_sec REAL NOT NULL,
  source TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (asset_id, draft_hash)
);
CREATE INDEX IF NOT EXISTS idx_narration_reviews_asset ON narration_reviews(asset_id);
