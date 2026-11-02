-- 回滚：删除 3 个中等难度剧本种子数据
DELETE FROM defects WHERE code IN ('slow-query-order-01', 'net-loss-product-01', 'mem-leak-user-01') AND project_id = 1;
