-- 沙箱实例（《数据库设计文档》§8.1，P0），sandbox-service 属主表。
-- user_id / project_id 逻辑归属 user-service / content-service；session_id 与
-- training_sessions.sandbox_id 构成跨服务循环引用。共库下一律不建物理外键，仅建索引，
-- 由各服务在编排层保证一致性（创建/销毁与训练会话联动）。
CREATE TABLE IF NOT EXISTS sandboxes (
    id               BIGSERIAL PRIMARY KEY,
    uuid             VARCHAR(36)  NOT NULL UNIQUE, -- 用作 K8s namespace
    project_id       BIGINT       NOT NULL,
    user_id          BIGINT       NOT NULL,
    session_id       BIGINT,
    cluster_id       VARCHAR(50)  NOT NULL DEFAULT '',
    namespace        VARCHAR(100) NOT NULL DEFAULT '',
    status           VARCHAR(30)  NOT NULL DEFAULT 'creating', -- creating/running/paused/destroying/destroyed/failed
    speed_multiplier DECIMAL(3,1) NOT NULL DEFAULT 1.0,         -- MVP 固定 1.0
    resource_profile VARCHAR(20)  NOT NULL DEFAULT 'standard',  -- economy/standard/large
    client_type      VARCHAR(30)  NOT NULL DEFAULT 'web',
    access_url       VARCHAR(500),
    monitoring_url   VARCHAR(500),
    log_url          VARCHAR(500),
    terminal_url     VARCHAR(500),
    elastic_enabled  BOOLEAN      NOT NULL DEFAULT true,
    elastic_frozen   BOOLEAN      NOT NULL DEFAULT false,
    frozen_services  JSONB,
    tenant_id        BIGINT       NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    started_at       TIMESTAMPTZ,
    last_heartbeat_at TIMESTAMPTZ,
    destroyed_at     TIMESTAMPTZ,
    destroy_reason   VARCHAR(50),  -- completed/timeout/idle/manual/error
    error_message    TEXT,
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sandboxes_user_id       ON sandboxes (user_id);
CREATE INDEX IF NOT EXISTS idx_sandboxes_session_id    ON sandboxes (session_id);
CREATE INDEX IF NOT EXISTS idx_sandboxes_status        ON sandboxes (status);
CREATE INDEX IF NOT EXISTS idx_sandboxes_last_heartbeat ON sandboxes (last_heartbeat_at);
CREATE INDEX IF NOT EXISTS idx_sandboxes_created_at    ON sandboxes (created_at DESC);
