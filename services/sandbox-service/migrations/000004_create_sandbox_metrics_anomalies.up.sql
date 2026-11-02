-- 沙箱资源指标快照（《数据库设计文档》§8.x，P0）。
-- sandbox-service 属主表：MonitorService 采集写入，管理后台趋势图读取。
-- 时序数据量大，MVP 不做分表，仅按 session_id + 时间建索引；后续可接 Prometheus 替代。
CREATE TABLE IF NOT EXISTS sandbox_metrics (
    id             BIGSERIAL PRIMARY KEY,
    session_id     BIGINT       NOT NULL,
    namespace      VARCHAR(100) NOT NULL DEFAULT '',
    cpu_cores_m    DOUBLE PRECISION NOT NULL DEFAULT 0, -- 毫核
    cpu_percent    DOUBLE PRECISION NOT NULL DEFAULT 0, -- 0-100
    memory_bytes   BIGINT       NOT NULL DEFAULT 0,
    memory_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
    pod_count      INT          NOT NULL DEFAULT 0,
    pod_running    INT          NOT NULL DEFAULT 0,
    pod_abnormal   INT          NOT NULL DEFAULT 0,
    tenant_id      BIGINT       NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()  -- 采集时间
);

CREATE INDEX IF NOT EXISTS idx_sandbox_metrics_session_time
    ON sandbox_metrics (session_id, created_at DESC);

-- 沙箱异常告警记录（MonitorService 异常检测写入）。
CREATE TABLE IF NOT EXISTS sandbox_anomalies (
    id           BIGSERIAL PRIMARY KEY,
    session_id   BIGINT       NOT NULL,
    namespace    VARCHAR(100) NOT NULL DEFAULT '',
    user_id      BIGINT       NOT NULL DEFAULT 0,
    anomaly_type VARCHAR(50)  NOT NULL, -- high_cpu/high_memory/long_running/failed_pod/stuck_status/oom
    level        VARCHAR(20)  NOT NULL, -- warning/error
    message      TEXT         NOT NULL DEFAULT '',
    snapshot     JSONB,                 -- 异常时资源快照（ResourceMetrics）
    resolved     BOOLEAN      NOT NULL DEFAULT false,
    detected_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ,
    tenant_id    BIGINT       NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sandbox_anomalies_session ON sandbox_anomalies (session_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_anomalies_resolved ON sandbox_anomalies (resolved);
CREATE INDEX IF NOT EXISTS idx_sandbox_anomalies_detected ON sandbox_anomalies (detected_at DESC);
