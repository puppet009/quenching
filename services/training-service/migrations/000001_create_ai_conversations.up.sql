-- AI 对话表（T2-27）。字段与《数据库设计文档》11.1 对齐。
-- user_id 逻辑归属 user-service.users、session_id 关联本服务 training_sessions，
-- 其中 user_id 为跨服务引用，不建物理外键（共库下避免跨服务迁移顺序耦合），仅建索引；
-- 引用完整性由会话/用户编排层保证。
CREATE TABLE IF NOT EXISTS ai_conversations (
    id                BIGSERIAL PRIMARY KEY,
    session_id        BIGINT,
    user_id           BIGINT NOT NULL,
    conversation_type VARCHAR(30) NOT NULL DEFAULT 'training_assistant',
    ai_model          VARCHAR(50) NOT NULL DEFAULT '',
    system_prompt     TEXT,
    prompt_version    VARCHAR(20),
    status            VARCHAR(20) NOT NULL DEFAULT 'active', -- active/closed/error
    message_count     INT NOT NULL DEFAULT 0,
    total_tokens      INT NOT NULL DEFAULT 0,
    total_cost        DECIMAL(10,6) NOT NULL DEFAULT 0,
    context_data      JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_conversations_user_id    ON ai_conversations(user_id);
CREATE INDEX IF NOT EXISTS idx_ai_conversations_session_id ON ai_conversations(session_id);
