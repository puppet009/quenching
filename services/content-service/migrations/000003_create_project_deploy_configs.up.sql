-- 内容域：project_deploy_configs 表
-- 定义项目在K8s中的部署配置，沙箱创建时读取此配置生成部署清单。
CREATE TABLE IF NOT EXISTS project_deploy_configs (
    id                      BIGSERIAL PRIMARY KEY,
    project_id              BIGINT       NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    k8s_namespace_template  VARCHAR(100) NOT NULL DEFAULT '',
    helm_chart              VARCHAR(500) NOT NULL DEFAULT '',
    helm_values             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    ingress_config          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    storage_config          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    network_policy          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    init_script             TEXT         NOT NULL DEFAULT '',
    health_check            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    access_layer_config     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_deploy_configs_project_id ON project_deploy_configs (project_id);
