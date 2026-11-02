-- 修复 000004 预置 easy 剧本的 amplifier_config 枚举漂移。
--
-- 背景：000004 播种时使用了 cpu_load / disk_fill 两个故障类型，以及
-- spec.path / spec.size_percent 两个字段；当前领域代码（model.Kind*、
-- blade.BuildCreateArgs、scenario.Parse supportedKind）只认：
--   cpu / memory / disk / network_delay / network_loss / process / threadpool
-- 磁盘填充强度字段为 spec.disk_percent，填充路径在 blade 命令构造中固定为 "/"。
-- 不修这两行，scenario.Parse/自动注入会直接报错。
--
-- 本迁移幂等：jsonb_set 按 code 精确改写；新环境若已用修正后的 000004 播种，
-- 更新结果与现值相同，重复执行无副作用。

-- 剧本#1：cpu_load -> cpu
UPDATE defects
SET amplifier_config = jsonb_set(
      jsonb_set(amplifier_config, '{kind}', '"cpu"'),
      '{spec,kind}', '"cpu"'
    ),
    updated_at = now()
WHERE code = 'cpu-stress-order-01'
  AND amplifier_config->>'kind' = 'cpu_load';

-- 剧本#2：disk_fill -> disk；path/size_percent -> disk_percent
-- 直接用重建后的完整 JSON 替换，避免对已不存在的 path 键做删除分支。
UPDATE defects
SET amplifier_config = '{"code":"disk-fill-product-01","kind":"disk","title":"商品服务磁盘打满","target":{"pod":"product-service"},"spec":{"kind":"disk","disk_percent":95},"trigger":{"type":"delay","delay_seconds":60},"recovery":[{"metric":"disk_usage","service":"product-service","op":"lte","value":80}],"auto_revoke":true}'::jsonb,
    updated_at = now()
WHERE code = 'disk-fill-product-01'
  AND amplifier_config->>'kind' = 'disk_fill';
