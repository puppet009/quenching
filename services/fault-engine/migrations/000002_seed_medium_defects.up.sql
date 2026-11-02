-- 中等难度故障剧本种子数据（剧本#3-#5）
-- 对应 T2-26：慢查询致延迟、网络丢包致级联超时、内存泄漏
-- 项目：电商演示项目(project_id=1, 假设存在)
-- 注意：amplifier_config 为 ChaosBlade 注入配置，JSON 结构与 scenario.Scenario 一致

-- 剧本#3：慢查询致延迟
-- 根因：order-service 数据库查询未加索引，大表扫描导致延迟
-- 表现：订单接口 P99 飙升，错误率上升
-- 修复：添加索引 / 优化 SQL
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'slow-query-order-01',
  '订单慢查询致延迟',
  'code',
  'database',
  'medium',
  '订单服务数据库查询慢，导致下单接口超时',
  'order-service 查询订单列表时未加索引，全表扫描导致延迟飙升，级联影响下单主流程',
  3,
  '{"type":"delay","delay_seconds":60}',
  '{"metrics":[{"name":"p99_latency","service":"order-svc","op":"gt","value":500}],"logs":[{"keyword":"slow query","service":"order-svc"}]}',
  '{"steps":[{"step":1,"description":"定位慢查询 SQL"},{"step":2,"description":"添加索引优化"}],"verify":"订单接口 P99 恢复正常"}',
  '{"code":"slow-query-order-01","kind":"network_delay","title":"订单慢查询致延迟","target":{"pod":"order-svc","network_interface":"eth0"},"spec":{"kind":"network_delay","delay_millis":300},"trigger":{"type":"delay","delay_seconds":60},"recovery":[{"metric":"p99_latency","service":"order-svc","op":"lte","value":200},{"metric":"error_rate","service":"order-svc","op":"lte","value":0.01}],"auto_revoke":true}',
  15,
  true,
  10
);

-- 剧本#4：网络丢包致级联超时
-- 根因：product-service 网络丢包，order 调用 product 超时
-- 表现：下单成功率下降，order 服务错误率飙升
-- 修复：排查网络 / 增加重试 / 降级
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'net-loss-product-01',
  '商品服务网络丢包致级联超时',
  'config',
  'network',
  'medium',
  '商品服务网络丢包导致订单服务调用超时，下单成功率骤降',
  'product-service 网络丢包率高，order-service 调用 product 时频繁超时，级联导致下单失败率飙升',
  2,
  '{"type":"metric","metric":{"metric":"qps","service":"order-svc","op":"gt","value":20}}',
  '{"metrics":[{"name":"error_rate","service":"order-svc","op":"gt","value":0.1}],"alerts":[{"level":"critical","message":"下单成功率低于90%"}]}',
  '{"steps":[{"step":1,"description":"确认是网络问题而非代码问题"},{"step":2,"description":"排查网络链路/配置"}],"verify":"下单成功率恢复到99%以上"}',
  '{"code":"net-loss-product-01","kind":"network_loss","title":"商品服务网络丢包","target":{"pod":"product-svc","network_interface":"eth0"},"spec":{"kind":"network_loss","loss_percent":30},"trigger":{"type":"metric","metric":{"metric":"qps","service":"order-svc","op":"gt","value":20}},"recovery":[{"metric":"error_rate","service":"order-svc","op":"lte","value":0.01},{"metric":"p99_latency","service":"order-svc","op":"lte","value":300}],"auto_revoke":true}',
  20,
  true,
  11
);

-- 剧本#5：内存泄漏
-- 根因：user-service 内存泄漏，导致 GC 频繁、服务响应变慢
-- 表现：user 服务内存持续上涨，最终 OOM
-- 修复：定位泄漏点 / 释放资源
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'mem-leak-user-01',
  '用户服务内存泄漏',
  'code',
  'memory',
  'medium',
  '用户服务内存持续泄漏，最终 OOM 重启',
  'user-service 缓存未设置过期策略，内存持续增长，GC 频率增加，最终 OOM 导致服务不可用',
  1,
  '{"type":"delay","delay_seconds":120}',
  '{"metrics":[{"name":"memory_usage","service":"user-svc","op":"gt","value":0.8}],"alerts":[{"level":"warning","message":"用户服务内存使用率超过80%"}]}',
  '{"steps":[{"step":1,"description":"通过监控确认内存持续上涨"},{"step":2,"description":"heap dump 分析泄漏点"}],"verify":"内存使用率稳定在正常水平"}',
  '{"code":"mem-leak-user-01","kind":"memory","title":"用户服务内存泄漏","target":{"pod":"user-svc"},"spec":{"kind":"memory","mem_percent":85},"trigger":{"type":"delay","delay_seconds":120},"recovery":[{"metric":"memory_usage","service":"user-svc","op":"lte","value":0.7}],"auto_revoke":true}',
  25,
  true,
  12
);
