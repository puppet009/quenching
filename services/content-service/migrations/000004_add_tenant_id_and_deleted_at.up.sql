-- 补齐《数据库设计文档》§1.1 的两条全库约定列：tenant_id（B/G 端多租户预留）、deleted_at（软删除）。
-- 已落地的表此前全部缺这两列，本迁移按服务域各自补齐；此后新建表必须自带。
-- 不加索引：MVP 阶段 tenant_id 恒为 0，单列 B-tree 不会被优化器选中；
--   deleted_at 的部分索引等有真实列表查询时再按 §13 补。
-- 不加 COMMENT：两列语义由 §1.1 单点定义，抄到每张表正是本轮文档与实现漂移的成因。
-- 行为不变：仓储层尚无 WHERE deleted_at IS NULL 过滤，content-service 三处仍是硬 DELETE，
--   接入软删除是代码改动，不在本迁移范围内。
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS tenant_id  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE project_services
    ADD COLUMN IF NOT EXISTS tenant_id  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE project_deploy_configs
    ADD COLUMN IF NOT EXISTS tenant_id  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
