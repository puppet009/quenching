-- 复盘报告（《数据库设计文档》§10.1，P0），review-service 属主表。
-- session_id / user_id / project_id 分别逻辑归属 training-service / user-service /
-- content-service，均为跨服务引用，共库下不建物理外键，仅建索引；一会话一份复盘，
-- session_id 加唯一约束。章节明细 review_sections 为 P1，后续迁移再建。
CREATE TABLE IF NOT EXISTS reviews (
    id                        BIGSERIAL PRIMARY KEY,
    session_id                BIGINT       NOT NULL UNIQUE,
    user_id                   BIGINT       NOT NULL,
    project_id                BIGINT       NOT NULL,
    title                     VARCHAR(200),
    summary                   TEXT,
    overall_evaluation        TEXT,
    review_level              VARCHAR(20)  NOT NULL DEFAULT 'basic', -- basic/method/architecture
    generation_status         VARCHAR(20)  NOT NULL DEFAULT 'generating', -- generating/completed/failed
    ai_model                  VARCHAR(50),
    prompt_version            VARCHAR(20),
    generation_duration       INT,                                    -- 毫秒
    tech_score                DECIMAL(6,2),
    collab_score              DECIMAL(6,2),
    question_quality_score    DECIMAL(5,2),
    judgment_score            DECIMAL(5,2),
    error_recognition_rate    DECIMAL(5,2),
    evidence_validation_rate  DECIMAL(5,2),
    ai_pitfall_count          SMALLINT     NOT NULL DEFAULT 0,
    ai_pitfall_recognized     SMALLINT     NOT NULL DEFAULT 0,
    ai_pitfall_analysis       JSONB,
    view_count                INT          NOT NULL DEFAULT 0,
    is_shared                 BOOLEAN      NOT NULL DEFAULT false,
    share_token               VARCHAR(32),
    user_rating               SMALLINT,
    user_feedback             TEXT,
    tenant_id                 BIGINT       NOT NULL DEFAULT 0,
    created_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at                TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_reviews_user_id    ON reviews (user_id);
CREATE INDEX IF NOT EXISTS idx_reviews_project_id ON reviews (project_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_reviews_share_token ON reviews (share_token);
