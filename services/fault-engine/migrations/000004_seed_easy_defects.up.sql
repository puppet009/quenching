-- 简单难度故障剧本种子数据（剧本#1-#2）
-- 对应 T2-25：CPU满载、磁盘打满
-- 项目：电商演示项目(project_id=1, 假设存在)
-- 注意：amplifier_config 为 ChaosBlade 注入配置，JSON 结构与 scenario.Scenario 一致

-- 剧本#1：CPU 满载
-- 根因：order-service CPU 被恶意进程占满，服务响应变慢
-- 表现：CPU 使用率 100%，接口延迟飙升，QPS 下降
-- 修复：找到异常进程并终止 / 扩容 CPU 资源
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, freeze_rules, score_weight, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'cpu-stress-order-01',
  '订单服务CPU满载',
  'resource',
  'cpu',
  'easy',
  '订单服务CPU使用率持续100%，接口响应变慢',
  'order-service 内有异常进程占用大量CPU资源，导致业务请求无法及时处理，表现为接口延迟飙升、QPS下降',
  3,
  '{"type":"delay","delay_seconds":30}',
  '{"metrics":[{"name":"cpu_usage","service":"order-service","op":"gt","value":90}],"alerts":[{"level":"critical","message":"订单服务CPU使用率超过90%"}]}',
  '{"steps":[{"step":1,"description":"通过监控确认CPU使用率异常"},{"step":2,"description":"使用top/ps定位高CPU进程"},{"step":3,"description":"终止异常进程或扩容资源"}],"verify":"CPU使用率回落至正常水平，接口延迟恢复"}',
  '{"code":"cpu-stress-order-01","kind":"cpu","title":"订单服务CPU满载","target":{"pod":"order-service"},"spec":{"kind":"cpu","cpu_percent":100},"trigger":{"type":"delay","delay_seconds":30},"recovery":[{"metric":"cpu_usage","service":"order-service","op":"lte","value":70}],"auto_revoke":true}',
  '{"max_cpu_percent":95,"stable_minutes":2}',
  1.0,
  10,
  true,
  1
);

-- 剧本#2：磁盘打满
-- 根因：product-service 日志文件未清理，磁盘被占满
-- 表现：磁盘使用率 100%，服务无法写入日志和数据
-- 修复：清理大日志文件 / 配置日志轮转 / 扩容磁盘
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, freeze_rules, score_weight, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'disk-fill-product-01',
  '商品服务磁盘打满',
  'resource',
  'disk',
  'easy',
  '商品服务磁盘被日志文件占满，服务无法正常写入',
  'product-service 日志文件未配置轮转策略，日积月累占满磁盘空间，导致服务无法写入新日志和数据，最终服务异常',
  2,
  '{"type":"delay","delay_seconds":60}',
  '{"metrics":[{"name":"disk_usage","service":"product-service","op":"gt","value":90}],"alerts":[{"level":"warning","message":"商品服务磁盘使用率超过90%"}]}',
  '{"steps":[{"step":1,"description":"通过监控确认磁盘使用率异常"},{"step":2,"description":"使用df/du定位大文件目录"},{"step":3,"description":"清理过期日志文件并配置轮转"}],"verify":"磁盘使用率回落至正常水平，服务恢复正常写入"}',
  '{"code":"disk-fill-product-01","kind":"disk","title":"商品服务磁盘打满","target":{"pod":"product-service"},"spec":{"kind":"disk","disk_percent":95},"trigger":{"type":"delay","delay_seconds":60},"recovery":[{"metric":"disk_usage","service":"product-service","op":"lte","value":80}],"auto_revoke":true}',
  '{"max_disk_percent":90,"stable_minutes":2}',
  1.0,
  15,
  true,
  2
);
