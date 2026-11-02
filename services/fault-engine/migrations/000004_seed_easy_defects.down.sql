-- 回滚：删除简单难度故障剧本种子数据
DELETE FROM defects WHERE code IN ('cpu-stress-order-01', 'disk-fill-product-01');
