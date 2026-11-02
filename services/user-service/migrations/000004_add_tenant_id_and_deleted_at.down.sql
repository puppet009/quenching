-- 回滚 000004。
ALTER TABLE user_profiles DROP COLUMN IF EXISTS deleted_at, DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE users         DROP COLUMN IF EXISTS deleted_at, DROP COLUMN IF EXISTS tenant_id;
