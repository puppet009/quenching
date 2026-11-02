-- 补齐《数据库设计文档》§1.1 的全库约定列 tenant_id / deleted_at（含义与不加索引的理由见 content-service 000004）。
-- ai_conversations / ai_messages 是会话与消息流水，业务上本不该软删除；
-- 此处建列只为让"每张表都有两列"这条约定无例外，将来写通用 scope 过滤不必特判。
ALTER TABLE ai_conversations
    ADD COLUMN IF NOT EXISTS tenant_id  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE ai_messages
    ADD COLUMN IF NOT EXISTS tenant_id  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
