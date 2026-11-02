-- 回滚：删除 3 个进阶故障剧本种子数据
DELETE FROM defects WHERE code IN ('thread-pool-exhaust-order-01', 'cache-avalanche-product-01', 'config-error-db-pool-01') AND project_id = 1;
