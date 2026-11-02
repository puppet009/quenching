-- 困难/进阶故障剧本种子数据（剧本#6-#8）
-- 对应 T3-32：线程池耗尽、缓存击穿/雪崩、配置错误
-- 项目：电商演示项目(project_id=1, 假设存在)
-- 注意：amplifier_config 为 ChaosBlade 注入配置，JSON 结构与 scenario.Scenario 一致

-- 剧本#6：线程池耗尽
-- 根因：order-service 业务线程池耗尽，请求堆积导致 503
-- 表现：线程池活跃线程 100%，请求排队，503 错误率飙升
-- 修复：调大线程池 / 排查阻塞点 / 限流降级
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'thread-pool-exhaust-order-01',
  '订单服务线程池耗尽',
  'architecture',
  'concurrency',
  'hard',
  '订单服务线程池耗尽，请求堆积导致大面积 503 错误',
  'order-service 业务线程池配置过小，高峰期请求量超过线程池容量，新请求被拒绝，503 错误率飙升。同时队列堆积导致延迟增大，级联影响上游服务。',
  3,
  '{"type":"metric","metric":{"metric":"qps","service":"order-svc","op":"gt","value":50}}',
  '{"metrics":[{"name":"active_threads","service":"order-svc","op":"eq","value":1.0,"description":"线程池使用率100%"},{"name":"error_rate_503","service":"order-svc","op":"gt","value":0.2},{"name":"queue_size","service":"order-svc","op":"gt","value":100}],"logs":[{"keyword":"Thread pool is exhausted","service":"order-svc"},{"keyword":"rejected execution","service":"order-svc"}],"alerts":[{"level":"critical","message":"订单服务线程池耗尽"}]}',
  '{"steps":[{"step":1,"description":"监控发现线程池使用率 100%，确认是线程池耗尽问题"},{"step":2,"description":"通过 jstack/线程 dump 分析线程阻塞点，定位是 DB 查询慢导致线程占住"},{"step":3,"description":"临时调大线程池参数，缓解压力"},{"step":4,"description":"优化慢查询 / 增加缓存，从根本上降低线程占用时间"}],"verify":"线程池使用率下降到 70% 以下，503 错误率归零"}',
  '{"code":"thread-pool-exhaust-order-01","kind":"thread_pool","title":"订单服务线程池耗尽","target":{"pod":"order-svc"},"spec":{"kind":"thread_pool","pool_name":"business-pool","core_size":10,"max_size":10,"queue_capacity":50,"fill_delay_seconds":30},"trigger":{"type":"metric","metric":{"metric":"qps","service":"order-svc","op":"gt","value":50}},"recovery":[{"metric":"active_threads","service":"order-svc","op":"lte","value":0.7},{"metric":"error_rate_503","service":"order-svc","op":"lte","value":0.01}],"auto_revoke":true}',
  30,
  true,
  13
);

-- 剧本#7：缓存击穿/雪崩
-- 根因：热点 key 批量过期，请求穿透到 DB，连接耗尽
-- 表现：Redis 命中率骤降，DB CPU/连接数飙升，服务响应变慢
-- 修复：缓存永不过期 + 异步刷新 / 互斥锁 / 随机过期
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'cache-avalanche-product-01',
  '商品缓存击穿致 DB 雪崩',
  'config',
  'cache',
  'hard',
  '商品热点缓存批量过期，请求穿透致数据库连接耗尽',
  'product-service 大量商品详情缓存 key 设置了相同的过期时间，到期时全部失效，海量请求直接穿透到数据库，DB 连接池瞬间耗尽，新查询全部排队超时，形成雪崩效应。',
  2,
  '{"type":"delay","delay_seconds":60}',
  '{"metrics":[{"name":"redis_hit_rate","service":"product-svc","op":"lt","value":0.3,"description":"Redis 命中率低于30%"},{"name":"db_connection_usage","service":"product-db","op":"gt","value":0.9},{"name":"db_cpu","service":"product-db","op":"gt","value":0.85},{"name":"p99_latency","service":"product-svc","op":"gt","value":1000}],"logs":[{"keyword":"cache miss","service":"product-svc"},{"keyword":"connection pool exhausted","service":"product-db"}],"alerts":[{"level":"critical","message":"数据库连接池耗尽"}]}',
  '{"steps":[{"step":1,"description":"监控发现 Redis 命中率骤降，DB 连接池耗尽，确认是缓存雪崩"},{"step":2,"description":"紧急措施：服务限流 + 热点数据预热回缓存"},{"step":3,"description":"排查缓存过期策略，发现大量 key 同时到期"},{"step":4,"description":"修复方案：缓存过期时间加随机偏移，避免集中失效；热点 key 永不过期+后台异步刷新"}],"verify":"Redis 命中率恢复到 90% 以上，DB 连接使用率正常"}',
  '{"code":"cache-avalanche-product-01","kind":"cache","title":"商品缓存雪崩","target":{"redis":"redis-0","key_pattern":"product:detail:*"},"spec":{"kind":"cache_invalidation","key_pattern":"product:detail:*","batch_size":100,"invalidate_delay_seconds":5},"trigger":{"type":"delay","delay_seconds":60},"recovery":[{"metric":"redis_hit_rate","service":"product-svc","op":"gte","value":0.8},{"metric":"db_connection_usage","service":"product-db","op":"lte","value":0.6}],"auto_revoke":true}',
  35,
  true,
  14
);

-- 剧本#8：配置错误
-- 根因：order-service 数据库连接池配置错误（最大连接数设为 0），服务启动后无法连接 DB
-- 表现：服务启动报错，DB 连接失败，所有 DB 操作均抛异常
-- 修复：修正连接池配置 / 检查环境变量
INSERT INTO defects (project_id, code, name, defect_type, category, difficulty, description, root_cause, target_service_id, trigger_config, manifestation_config, fix_config, amplifier_config, estimated_fix_time, is_active, sort_order)
VALUES (
  1,
  'config-error-db-pool-01',
  '数据库连接池配置错误',
  'config',
  'config',
  'easy',
  '数据库连接池最大连接数配置为 0，服务无法访问数据库',
  'order-service 配置文件中数据库连接池 maximum-pool-size 被误设为 0（或环境变量覆盖错误），服务启动后无法获取数据库连接，所有涉及 DB 的请求全部失败。',
  3,
  '{"type":"immediate"}',
  '{"metrics":[{"name":"db_active_connections","service":"order-svc","op":"eq","value":0},{"name":"error_rate","service":"order-svc","op":"gt","value":0.5}],"logs":[{"keyword":"maxPoolSize is 0","service":"order-svc"},{"keyword":"cannot obtain connection","service":"order-svc"},{"keyword":"SQLException","service":"order-svc"}],"alerts":[{"level":"critical","message":"订单服务数据库连接异常"}]}',
  '{"steps":[{"step":1,"description":"查看错误日志，发现数据库连接异常，提示连接池大小为 0"},{"step":2,"description":"检查 application.yml 配置，发现 maximum-pool-size 配置错误"},{"step":3,"description":"修正配置，重新发布服务"}],"verify":"数据库连接数正常，请求成功率恢复 99%+"}',
  '{"code":"config-error-db-pool-01","kind":"config","title":"数据库连接池配置错误","target":{"pod":"order-svc","config_file":"application.yml"},"spec":{"kind":"config_modify","config_file":"application.yml","key":"spring.datasource.hikari.maximum-pool-size","value":"0"},"trigger":{"type":"immediate"},"recovery":[{"metric":"db_active_connections","service":"order-svc","op":"gte","value":5},{"metric":"error_rate","service":"order-svc","op":"lte","value":0.01}],"auto_revoke":false}',
  10,
  true,
  15
);
