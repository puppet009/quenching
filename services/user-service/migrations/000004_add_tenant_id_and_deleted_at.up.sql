-- 补齐《数据库设计文档》§1.1 的全库约定列 tenant_id / deleted_at（含义与不加索引的理由见 content-service 000004）。
-- user_profiles 是 1:1 扩展表，两列实际随 users 走；仍一并建上，为了将来写通用 scope 过滤时不必为它开例外。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS tenant_id  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE user_profiles
    ADD COLUMN IF NOT EXISTS tenant_id  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
