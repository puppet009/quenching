-- 回滚因果误判类坑点种子数据（T3-15）
DELETE FROM ai_pitfall_configs
WHERE pitfall_type = 'causal_misjudgment'
  AND defect_id IN (1, 2, 3)
  AND name IN (
    '流量突增导致延迟',
    '订单服务代码bug',
    '流量大导致内存高'
  );
