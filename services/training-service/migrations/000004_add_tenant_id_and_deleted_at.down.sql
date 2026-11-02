-- 回滚 000004。
ALTER TABLE ai_messages      DROP COLUMN IF EXISTS deleted_at, DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE ai_conversations DROP COLUMN IF EXISTS deleted_at, DROP COLUMN IF EXISTS tenant_id;
