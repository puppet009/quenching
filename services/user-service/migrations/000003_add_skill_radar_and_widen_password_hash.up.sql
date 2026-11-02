-- 补齐 user_profiles 的技术维（skill_radar / avg_score），使其与已有的协作维成对。
-- 依据《数据库设计文档》§5.2 与 MVP需求文档验收标准#11「双维雷达」；无独立表、无迁移数据。
-- password_hash 放宽到 255 以对齐文档，为将来换 argon2id 留出长度（哈希可达 97+ 字符）。
ALTER TABLE user_profiles
    ADD COLUMN IF NOT EXISTS skill_radar  JSONB,
    ADD COLUMN IF NOT EXISTS avg_score    DECIMAL(5,2);

COMMENT ON COLUMN user_profiles.skill_radar IS '排障技术能力雷达数据（P0 展示字段；按维度筛选的 GIN 索引 MVP 不建，见文档 §13）';
COMMENT ON COLUMN user_profiles.avg_score   IS '平均技术评分，与 avg_collab_score 构成双轴';

ALTER TABLE users ALTER COLUMN password_hash TYPE VARCHAR(255);
