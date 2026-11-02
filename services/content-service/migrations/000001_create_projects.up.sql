-- 内容域：projects 表（CRUD 参考实现）
-- 完整字段以《数据库设计文档》为准，这里给出可跑通范式的最小集。
CREATE TABLE IF NOT EXISTS projects (
    id            BIGSERIAL PRIMARY KEY,
    name          VARCHAR(100) NOT NULL,
    domain        VARCHAR(50)  NOT NULL DEFAULT '',
    tech_stack    JSONB        NOT NULL DEFAULT '[]'::jsonb,
    deploy_config JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status        VARCHAR(20)  NOT NULL DEFAULT 'online',
    sort_order    INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_projects_domain ON projects (domain);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects (status);
