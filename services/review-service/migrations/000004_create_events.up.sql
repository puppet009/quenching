-- 分析事件表
-- 存储所有埋点事件数据，用于漏斗分析和行为统计。

CREATE TABLE IF NOT EXISTS events (
    id           BIGSERIAL PRIMARY KEY,
    event_type   VARCHAR(64) NOT NULL,          -- 事件类型
    user_id      BIGINT NOT NULL DEFAULT 0,      -- 用户ID（0=匿名）
    session_id   BIGINT,                         -- 训练会话ID
    project_id   BIGINT,                         -- 项目ID

    -- 事件属性（JSONB，灵活扩展）
    properties   JSONB DEFAULT '{}'::JSONB,

    -- 客户端信息
    client_ip    VARCHAR(64),
    user_agent   TEXT,
    platform     VARCHAR(32),                    -- web/mobile/desktop

    occurred_at  TIMESTAMPTZ NOT NULL,           -- 事件发生时间（客户端）
    received_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- 服务端接收时间

    -- 索引
    CONSTRAINT chk_event_type_not_empty CHECK (event_type <> '')
);

-- 事件类型 + 时间索引（用于按类型统计）
CREATE INDEX IF NOT EXISTS idx_events_event_type_time ON events (event_type, occurred_at);

-- 用户 + 事件类型索引（用于唯一事件去重检查）
CREATE INDEX IF NOT EXISTS idx_events_user_event ON events (user_id, event_type);

-- 时间索引（用于时间范围查询）
CREATE INDEX IF NOT EXISTS idx_events_occurred_at ON events (occurred_at);

-- 唯一事件约束：每个用户每个唯一事件只记录一条
-- 用部分索引实现：对 register/first_start/first_complete 等唯一事件建立 (user_id, event_type) 唯一索引
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_unique_user_event
ON events (user_id, event_type)
WHERE event_type IN ('register', 'first_start', 'first_complete');

COMMENT ON TABLE events IS '埋点事件表';
COMMENT ON COLUMN events.event_type IS '事件类型：register/first_start/first_complete/retrain/login/...';
COMMENT ON COLUMN events.properties IS '事件属性，JSONB 格式，灵活扩展';
