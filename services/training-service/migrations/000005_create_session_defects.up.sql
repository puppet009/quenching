-- 会话缺陷状态（《数据库设计文档》§9.2，P0）。
-- session_id 为服务内引用 training_sessions，建物理外键；defect_id 跨服务引用
-- fault-engine.defects，不建物理外键，仅随唯一索引覆盖查询。
CREATE TABLE IF NOT EXISTS session_defects (
    id               BIGSERIAL PRIMARY KEY,
    session_id       BIGINT       NOT NULL REFERENCES training_sessions(id) ON DELETE CASCADE,
    defect_id        BIGINT       NOT NULL,
    status           VARCHAR(30)  NOT NULL DEFAULT 'dormant', -- dormant/triggered/identified/fixed/timeout
    triggered_at     TIMESTAMPTZ,
    identified_at    TIMESTAMPTZ,
    fixed_at         TIMESTAMPTZ,
    time_to_trigger  INT,                                      -- 秒
    time_to_identify INT,
    time_to_fix      INT,
    fix_attempts     SMALLINT     NOT NULL DEFAULT 0,
    penalty          DECIMAL(5,2) NOT NULL DEFAULT 0,
    score            DECIMAL(5,2),
    fix_path         JSONB,
    trigger_details  JSONB,
    tenant_id        BIGINT       NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_session_defects_session_defect_uniq
    ON session_defects (session_id, defect_id);
CREATE INDEX IF NOT EXISTS idx_session_defects_session_id ON session_defects (session_id);
CREATE INDEX IF NOT EXISTS idx_session_defects_status     ON session_defects (status);
