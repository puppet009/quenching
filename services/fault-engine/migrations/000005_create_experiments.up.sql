-- 故障注入实验记录（T1-26 / T2-13 / T3-12）。
-- 每次经 ChaosBlade 的注入都落一行：
--   source='scenario'    剧本链路，按 defects.amplifier_config 由编排器自动注入；
--   source='consequence' 临时链路，training-service 踩坑后果传导直注。
-- session_id 逻辑归属 training-service（训练会话），属跨服务引用，不建物理外键，
-- 仅建索引；experiment_uid 是 blade create 返回的恢复凭证，撤销时必用。
CREATE TABLE IF NOT EXISTS experiments (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT       NOT NULL,
    source          VARCHAR(20)  NOT NULL DEFAULT 'scenario',  -- scenario/consequence
    kind            VARCHAR(30)  NOT NULL,
    namespace       VARCHAR(200) NOT NULL DEFAULT '',          -- 注入目标命名空间（冗余自 spec，便于排查）
    pod             VARCHAR(200) NOT NULL DEFAULT '',          -- 注入目标 Pod
    spec            JSONB        NOT NULL,                     -- model.InjectSpec 完整规格（撤销/补偿时重放）
    command         TEXT         NOT NULL DEFAULT '',          -- 实际执行的 blade 命令（审计用）
    status          VARCHAR(20)  NOT NULL DEFAULT 'created',   -- created/running/revoked/failed
    experiment_uid  VARCHAR(100) NOT NULL DEFAULT '',          -- blade 实验 uid，destroy 时用
    fail_reason     TEXT,
    tenant_id       BIGINT       NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 按会话查活跃注入（会话结束时批量撤销/崩溃补偿）。
CREATE INDEX IF NOT EXISTS idx_experiments_session_status ON experiments (session_id, status);
-- 按 uid 定位撤销（uid 全局唯一；应用层仍带 session 维度做归属校验）。
CREATE INDEX IF NOT EXISTS idx_experiments_uid ON experiments (experiment_uid);
