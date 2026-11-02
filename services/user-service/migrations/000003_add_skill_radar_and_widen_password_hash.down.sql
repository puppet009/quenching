-- 回滚 000003。
-- 注意：password_hash 回缩到 100 在存在超长哈希时会失败，这是有意为之（宁可失败也不截断）。
ALTER TABLE users ALTER COLUMN password_hash TYPE VARCHAR(100);

ALTER TABLE user_profiles
    DROP COLUMN IF EXISTS avg_score,
    DROP COLUMN IF EXISTS skill_radar;
