DROP INDEX IF EXISTS idx_training_sessions_template_slug;

ALTER TABLE training_sessions DROP COLUMN IF EXISTS template_slug;

-- 还原 project_id 约束：回填 0 后恢复 NOT NULL（回滚时新插入的空值以 0 兜底）。
UPDATE training_sessions SET project_id = 0 WHERE project_id IS NULL;

ALTER TABLE training_sessions
    ALTER COLUMN project_id SET NOT NULL;
