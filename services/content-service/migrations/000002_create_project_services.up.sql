-- 内容域：project_services 表
-- 定义项目包含的所有服务（微服务），每个服务有自己的镜像、端口、资源配置。
CREATE TABLE IF NOT EXISTS project_services (
    id                  BIGSERIAL PRIMARY KEY,
    project_id          BIGINT       NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                VARCHAR(100) NOT NULL,
    display_name        VARCHAR(100) NOT NULL DEFAULT '',
    service_type        VARCHAR(30)  NOT NULL DEFAULT 'app', -- app/database/cache/mq/gateway
    tech_stack          VARCHAR(50)  NOT NULL DEFAULT '',
    image               VARCHAR(500) NOT NULL,
    ports               JSONB        NOT NULL DEFAULT '[]'::jsonb,
    env_config          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    resource_default    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    resource_max        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    dependencies        JSONB        NOT NULL DEFAULT '[]'::jsonb,
    monitoring_config   JSONB        NOT NULL DEFAULT '{}'::jsonb,
    log_config          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    sort_order          SMALLINT     NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_services_project_id ON project_services (project_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_services_project_name ON project_services (project_id, name);
