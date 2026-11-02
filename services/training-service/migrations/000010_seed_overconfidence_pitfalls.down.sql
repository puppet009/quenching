-- 回滚过度自信类坑点种子数据（T3-17）
DELETE FROM ai_pitfall_configs
WHERE pitfall_type = 'overconfidence'
  AND defect_id IN (1, 2, 3)
  AND name IN (
    '缓存失效导致数据库压力',
    '连接池耗尽致级联失败',
    '大对象加载致内存突增'
  );
