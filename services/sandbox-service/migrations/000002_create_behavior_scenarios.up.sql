-- 行为模拟剧本（《数据库设计文档》§12.1，P0）。
-- 归属 sandbox-service：行为流量随沙箱 ready 启动、随沙箱释放停止（T1-29 流量开关）。
-- project_id 逻辑归属 content-service.projects，跨服务引用不建物理外键。
CREATE TABLE IF NOT EXISTS behavior_scenarios (
    id                     BIGSERIAL PRIMARY KEY,
    project_id             BIGINT       NOT NULL,
    name                   VARCHAR(100) NOT NULL,
    description            TEXT,
    scenario_type          VARCHAR(30)  NOT NULL DEFAULT 'steady', -- steady/growing/spike/daily_pattern/custom
    client_type            VARCHAR(30)  NOT NULL DEFAULT 'web',
    base_qps               INT          NOT NULL DEFAULT 10,
    peak_qps               INT,
    duration               INT          NOT NULL DEFAULT 3600,      -- 秒
    user_behavior_config   JSONB,
    traffic_pattern_config JSONB,
    api_endpoints          JSONB,
    is_default             BOOLEAN      NOT NULL DEFAULT false,
    is_active              BOOLEAN      NOT NULL DEFAULT true,
    tenant_id              BIGINT       NOT NULL DEFAULT 0,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at             TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_behavior_scenarios_project_id ON behavior_scenarios (project_id);
CREATE INDEX IF NOT EXISTS idx_behavior_scenarios_type       ON behavior_scenarios (scenario_type);
