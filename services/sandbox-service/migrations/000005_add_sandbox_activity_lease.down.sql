DROP INDEX IF EXISTS idx_sandboxes_completed_at;
ALTER TABLE sandboxes DROP COLUMN IF EXISTS completed_at;
