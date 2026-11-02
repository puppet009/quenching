-- 故障/缺陷配置（《数据库设计文档》§7.1，P0），fault-engine 属主表。
-- 大量结构化配置用 JSONB 承载。project_id / target_service_id 逻辑归属 content-service
-- （projects / project_services），属跨服务引用，不建物理外键，仅建索引，避免共库下的
-- 跨服务迁移顺序耦合；引用完整性由应用层与删除编排保证。
CREATE TABLE IF NOT EXISTS defects (
    id                  BIGSERIAL PRIMARY KEY,
    project_id          BIGINT       NOT NULL,
    code                VARCHAR(100) NOT NULL,
    name                VARCHAR(200) NOT NULL,
    defect_type         VARCHAR(30)  NOT NULL DEFAULT 'code',   -- code/config/architecture
    category            VARCHAR(50),                            -- database/cache/mq/network/cpu/memory...
    difficulty          VARCHAR(20)  NOT NULL DEFAULT 'easy',   -- easy/medium/hard
    description         TEXT,
    root_cause          TEXT,
    target_service_id   BIGINT,
    trigger_config      JSONB        NOT NULL,                  -- 触发条件配置
    manifestation_config JSONB       NOT NULL,                  -- 表现配置（指标/日志/告警）
    fix_config          JSONB        NOT NULL,                  -- 修复验证配置
    amplifier_config    JSONB,                                 -- 缺陷放大器（ChaosBlade 等）
    freeze_rules        JSONB,                                 -- 弹性冻结规则
    score_weight        DECIMAL(5,2) NOT NULL DEFAULT 1.0,
    estimated_fix_time  INT,                                    -- 预计修复时间（分钟）
    is_active           BOOLEAN      NOT NULL DEFAULT true,
    sort_order          SMALLINT     NOT NULL DEFAULT 0,
    tenant_id           BIGINT       NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_defects_project_code_uniq ON defects (project_id, code);
CREATE INDEX IF NOT EXISTS idx_defects_project_id        ON defects (project_id);
CREATE INDEX IF NOT EXISTS idx_defects_category          ON defects (category);
CREATE INDEX IF NOT EXISTS idx_defects_difficulty        ON defects (difficulty);
CREATE INDEX IF NOT EXISTS idx_defects_target_service    ON defects (target_service_id);
-- §13.3：按触发类型筛选缺陷，trigger_config 建 GIN（jsonb_ops）。
CREATE INDEX IF NOT EXISTS idx_defects_trigger_config_gin ON defects USING GIN (trigger_config);
