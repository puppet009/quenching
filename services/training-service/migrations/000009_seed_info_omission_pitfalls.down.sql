-- 回滚信息遗漏类坑点种子数据（T3-16）
DELETE FROM ai_pitfall_configs
WHERE pitfall_type = 'info_omission'
  AND defect_id IN (1, 2, 3)
  AND name IN (
    '只看应用层不提数据库',
    '只提上游不提下游网络',
    '只看堆内存不提GC'
  );
