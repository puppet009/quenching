-- AI 消息表（T2-27）。字段与《数据库设计文档》11.2 对齐。
-- 坑点相关字段（is_pitfall/pitfall_config_id/pitfall_type/confidence_level/
-- user_response_type）随 T3 坑点引擎启用，本迁移先建列，MVP 训练对话默认不写坑点。
CREATE TABLE IF NOT EXISTS ai_messages (
    id                BIGSERIAL PRIMARY KEY,
    conversation_id   BIGINT NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role              VARCHAR(20) NOT NULL,                 -- user/assistant/system
    content           TEXT NOT NULL,
    content_type      VARCHAR(20) NOT NULL DEFAULT 'text',  -- text/markdown/code/structured
    structured_data   JSONB,
    is_pitfall        BOOLEAN NOT NULL DEFAULT false,
    pitfall_config_id BIGINT,
    pitfall_type      VARCHAR(30),
    confidence_level  VARCHAR(10),
    user_response_type VARCHAR(20),
    tokens            INT,
    cost              DECIMAL(10,6),
    latency_ms        INT,
    tool_calls        JSONB,
    user_feedback     VARCHAR(20),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_messages_conversation_time
    ON ai_messages(conversation_id, created_at);
