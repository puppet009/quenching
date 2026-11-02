-- 会话操作时间线（《数据库设计文档》§9.3，P0）。结构化事件，写 PG 而非日志库（T1-23）。
-- session_id 为服务内引用，建物理外键。MVP 量大但不分区（文档：后期按 session 分区/归档）。
-- §13.3 明确 detail 的 GIN 索引 MVP 不建。
CREATE TABLE IF NOT EXISTS session_operations (
    id                 BIGSERIAL PRIMARY KEY,
    session_id         BIGINT      NOT NULL REFERENCES training_sessions(id) ON DELETE CASCADE,
    operation_type     VARCHAR(50) NOT NULL, -- view_metric/view_log/exec_command/restart_service/modify_config/ask_ai...
    operation_category VARCHAR(30),          -- observe/analyze/action/ai_assist
    target             VARCHAR(200),
    detail             JSONB,
    result             VARCHAR(20) NOT NULL DEFAULT 'executed', -- executed/success/failed/skipped
    penalty            DECIMAL(5,2) NOT NULL DEFAULT 0,
    virtual_timestamp  INT,                  -- 相对实训开始的真实经过秒数（MVP 不做倍速）
    tenant_id          BIGINT      NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_session_operations_session_time
    ON session_operations (session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_session_operations_type
    ON session_operations (operation_type);
CREATE INDEX IF NOT EXISTS idx_session_operations_category
    ON session_operations (operation_category);
