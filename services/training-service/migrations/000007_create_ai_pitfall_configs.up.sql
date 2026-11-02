-- AI 坑点配置（《数据库设计文档》§11.4，P0）。规则版坑点，后台可编辑，T3-8 训练启动时加载。
-- 归属 training-service：坑点引擎消费方在本服务，与 ai_conversations/ai_messages 同属 AI 交互域。
-- defect_id 逻辑归属 fault-engine.defects、created_by 归属 users，均为跨服务引用，
-- 不建物理外键，仅建索引；配置的级联有效性由后台/编排层保证。
CREATE TABLE IF NOT EXISTS ai_pitfall_configs (
    id                 BIGSERIAL PRIMARY KEY,
    defect_id          BIGINT       NOT NULL,
    pitfall_type       VARCHAR(30)  NOT NULL, -- causal_misjudgment/info_omission/overconfidence
    name               VARCHAR(100) NOT NULL,
    description        VARCHAR(500),
    trigger_condition  JSONB        NOT NULL, -- 时机+概率+前置状态
    error_content      TEXT         NOT NULL, -- AI 错误建议内容模板（Prompt 片段）
    confidence_level   VARCHAR(20)  NOT NULL DEFAULT 'random', -- low/medium/high/random
    recognition_rules  JSONB        NOT NULL, -- 何为“识别了坑”
    pitfall_rules      JSONB        NOT NULL, -- 何为“踩了坑”
    correct_approach   TEXT,
    severity           VARCHAR(10)  NOT NULL DEFAULT 'medium', -- low/medium/high
    status             VARCHAR(20)  NOT NULL DEFAULT 'active', -- draft/active/inactive
    sort_order         SMALLINT     NOT NULL DEFAULT 0,
    created_by         BIGINT,
    tenant_id          BIGINT       NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ai_pitfall_defect_id ON ai_pitfall_configs (defect_id);
CREATE INDEX IF NOT EXISTS idx_ai_pitfall_type     ON ai_pitfall_configs (pitfall_type);
CREATE INDEX IF NOT EXISTS idx_ai_pitfall_status   ON ai_pitfall_configs (status);
