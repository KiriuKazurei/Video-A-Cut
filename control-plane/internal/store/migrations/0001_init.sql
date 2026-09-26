CREATE TABLE IF NOT EXISTS assets (
  asset_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  agent_visible INTEGER NOT NULL DEFAULT 0,
  human_approved INTEGER NOT NULL DEFAULT 0,
  locked INTEGER NOT NULL DEFAULT 0,
  allowed_agents TEXT NOT NULL DEFAULT '[]',
  artifacts TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
  task_id TEXT PRIMARY KEY,
  asset_id TEXT NOT NULL,
  type TEXT NOT NULL,
  agent_role TEXT NOT NULL,
  agent_id TEXT,
  status TEXT NOT NULL,
  progress REAL NOT NULL DEFAULT 0,
  message TEXT,
  lease_expires_at DATETIME,
  claimed_at DATETIME,
  updated_at DATETIME NOT NULL,
  artifacts TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_tasks_role_status ON tasks(agent_role, status);
CREATE TABLE IF NOT EXISTS agents (
  agent_id TEXT PRIMARY KEY,
  role TEXT NOT NULL,
  last_seen DATETIME,
  current_task_id TEXT,
  health TEXT NOT NULL DEFAULT 'unknown'
);
CREATE TABLE IF NOT EXISTS audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  detail TEXT,
  created_at DATETIME NOT NULL
);
