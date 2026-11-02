-- T3-5 配套：训练会话支持按官方模板 slug 开训。
-- 模板已改为 templates/<slug>/template.yaml 文件注册表，不再依赖数据库 projects 主键，
-- 因此会话新增可空 template_slug，并放宽 project_id 的 NOT NULL（历史行与无模板裸会话以 0 占位）。
-- 两者至少需满足其一：project_id>0（数据库登记项目）或 template_slug 非空（文件模板），
-- 该校验由会话创建服务层保证。

ALTER TABLE training_sessions
    ADD COLUMN IF NOT EXISTS template_slug VARCHAR(40);

ALTER TABLE training_sessions
    ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE training_sessions
    ALTER COLUMN project_id SET DEFAULT 0;

UPDATE training_sessions SET project_id = 0 WHERE project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_training_sessions_template_slug
    ON training_sessions (template_slug);
