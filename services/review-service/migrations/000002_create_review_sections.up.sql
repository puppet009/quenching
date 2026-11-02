-- T2-30 复盘结构化与落库。
-- 1) 章节明细表（《数据库设计文档》§10.2）：一份复盘多章节，
--    content 存 Markdown 正文，content_data 预留结构化 JSONB；
--    review_id 逻辑归属 reviews，共库不建物理外键。
-- 2) 补齐 reviews 重试/错误信息两列：worker（T2-31）的
--    MarkGenerating/MarkFailed/IncrementRetry 已引用，000001 漏建。
CREATE TABLE IF NOT EXISTS review_sections (
    id            BIGSERIAL    PRIMARY KEY,
    review_id     BIGINT       NOT NULL,
    section_type  VARCHAR(50)  NOT NULL, -- 见 model 章节类型词表
    section_level VARCHAR(20)  NOT NULL DEFAULT 'basic', -- basic/method/arch
    title         VARCHAR(200) NOT NULL,
    content       TEXT,
    content_data  JSONB,
    sort_order    SMALLINT     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 报告页按章节顺序读取。
CREATE INDEX IF NOT EXISTS idx_review_sections_review_order
    ON review_sections (review_id, sort_order);

ALTER TABLE reviews ADD COLUMN IF NOT EXISTS error_message TEXT;
ALTER TABLE reviews ADD COLUMN IF NOT EXISTS retry_count    INT NOT NULL DEFAULT 0;
