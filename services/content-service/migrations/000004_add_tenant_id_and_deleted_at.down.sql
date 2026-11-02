-- 回滚 000004：移除全库约定列。
ALTER TABLE project_deploy_configs DROP COLUMN IF EXISTS deleted_at, DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE project_services       DROP COLUMN IF EXISTS deleted_at, DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE projects               DROP COLUMN IF EXISTS deleted_at, DROP COLUMN IF EXISTS tenant_id;
