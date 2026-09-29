-- Migration 0004: task orchestration.
--
-- depends_on is a JSON array of task ids that must all be succeeded before
-- this task can be claimed. attempts counts lease recoveries; the service
-- fails a task instead of requeueing it once the configured cap is reached,
-- so a task that kills every worker cannot cycle through the queue forever.
ALTER TABLE tasks ADD COLUMN depends_on TEXT NOT NULL DEFAULT '[]';
ALTER TABLE tasks ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
