-- 训练会话（《数据库设计文档》§9.1，P0），training-service 聚合根表。
-- user_id / project_id 逻辑归属 user-service / content-service；sandbox_id 与
-- sandboxes.session_id 构成跨服务循环引用。共库下均不建物理外键，仅建索引；
-- sandbox_id 一对一，加唯一约束。一致性由会话生命周期编排（T2-9 状态机）保证。
CREATE TABLE IF NOT EXISTS training_sessions (
    id                        BIGSERIAL PRIMARY KEY,
    tenant_id                 BIGINT      NOT NULL DEFAULT 0,
    user_id                   BIGINT      NOT NULL,
    project_id                BIGINT      NOT NULL,
    sandbox_id                BIGINT      UNIQUE,
    title                     VARCHAR(200),
    difficulty                VARCHAR(20) NOT NULL DEFAULT 'medium',  -- easy/medium/hard
    mode                      VARCHAR(30) NOT NULL DEFAULT 'practice', -- practice/challenge/exam
    ai_assist_level           VARCHAR(20) NOT NULL DEFAULT 'full',    -- 旧字段，向后兼容
    ai_mode                   VARCHAR(20) NOT NULL DEFAULT 'assistant', -- none/assistant/agent
    agent_level               VARCHAR(10) NOT NULL DEFAULT 'A1',       -- A0..A4（A轴）
    control_group             VARCHAR(5),                              -- A/B 交叉分组
    speed_multiplier          DECIMAL(3,1) NOT NULL DEFAULT 1.0,       -- MVP 固定 1.0
    status                    VARCHAR(30) NOT NULL DEFAULT 'preparing',
    defect_ids_active         JSONB,
    total_defect_count        SMALLINT    NOT NULL DEFAULT 0,
    fixed_defect_count        SMALLINT    NOT NULL DEFAULT 0,
    pitfall_total             SMALLINT    NOT NULL DEFAULT 0,
    pitfall_recognized        SMALLINT    NOT NULL DEFAULT 0,
    pitfall_fallen            SMALLINT    NOT NULL DEFAULT 0,
    evidence_validation_rate  DECIMAL(5,2),
    total_operation_count     INT         NOT NULL DEFAULT 0,
    total_score               DECIMAL(6,2),
    collab_score              DECIMAL(6,2),
    penalty_score             DECIMAL(6,2) NOT NULL DEFAULT 0,
    final_score               DECIMAL(6,2),
    grade                     VARCHAR(5),
    collab_grade              VARCHAR(5),
    time_limit                INT,                                     -- 秒，NULL=不限时
    actual_duration           INT,
    time_to_first_fix         INT,
    has_review                BOOLEAN     NOT NULL DEFAULT false,
    review_id                 BIGINT,
    started_at                TIMESTAMPTZ,
    ended_at                  TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at                TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_training_sessions_user_id      ON training_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_training_sessions_project_id   ON training_sessions (project_id);
CREATE INDEX IF NOT EXISTS idx_training_sessions_status       ON training_sessions (status);
CREATE INDEX IF NOT EXISTS idx_training_sessions_user_status  ON training_sessions (user_id, status);
CREATE INDEX IF NOT EXISTS idx_training_sessions_created_at   ON training_sessions (created_at DESC);
