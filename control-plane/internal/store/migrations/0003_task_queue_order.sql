-- Migration 0003: preserve queue arrival order independently from task_id.
-- Existing rows keep a NULL enqueue timestamp and are ordered by SQLite's
-- stable rowid; new rows receive an immutable timestamp from store.CreateTask.
ALTER TABLE tasks ADD COLUMN enqueued_at DATETIME;
CREATE INDEX IF NOT EXISTS idx_tasks_queue_order
  ON tasks(agent_role, status, enqueued_at);
