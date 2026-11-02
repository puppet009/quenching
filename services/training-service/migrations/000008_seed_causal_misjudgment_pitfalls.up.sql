-- 因果误判类坑点种子数据（T3-15）
-- 坑点类型：causal_misjudgment（把相关性当因果性）
-- 关联故障：剧本#3 订单慢查询（defect_id=1）、剧本#4 网络丢包（defect_id=2）、剧本#5 内存泄漏（defect_id=3）
-- 每个故障 1 个因果误判坑点，确保在对应故障场景中 100% 稳定出现
-- 设计原则：
--   1. 错误建议看似合理、有数据支撑，但因果关系颠倒或错误
--   2. 触发时机：用户开始询问根因/方向时（question_match）
--   3. 置信度：high，用"我分析了数据"增强说服力
--   4. 踩坑后果：中等严重，采纳后会引导到错误方向浪费时间（medium severity）
--   5. 识别信号：用户质疑/验证/换方向

-- ============================================================
-- 坑点 #1：订单慢查询 → "是流量突增导致的"
-- 场景：用户看到 P99 飙升 + 错误率上升，询问原因
-- 错误结论：把 QPS 上升和延迟升高的相关性当成因果，说是流量突增导致的
-- 正确方向：应该看慢查询日志 / 数据库指标，定位 SQL 问题
-- ============================================================
INSERT INTO ai_pitfall_configs (
  defect_id, pitfall_type, name, description,
  trigger_condition, error_content, confidence_level,
  recognition_rules, pitfall_rules, correct_approach,
  severity, status, sort_order
) VALUES (
  1,
  'causal_misjudgment',
  '流量突增导致延迟',
  '把 QPS 上升与延迟升高的相关性误判为因果，引导用户去做扩容而非优化 SQL',
  '{"when":"question_match","probability":1,"max_times":1,"question_keywords":["为什么延迟","延迟原因","根因是什么","怎么变慢了","p99 飙升","为什么超时","订单慢"],"requires":{"fault_triggered":true,"min_turn":1}}',
  '我分析了一下监控数据，订单服务的 QPS 在过去 10 分钟内上升了约 40%，同时 P99 延迟从 150ms 飙升到 800ms 以上，错误率也在上升。

这很明显是**流量突增导致的服务过载**。当前实例数不足以支撑现在的请求量，请求排队导致延迟飙升，最终超时。

建议你立即执行以下操作：
1. 先给 order-service 扩容 2-3 个实例缓解压力
2. 检查一下是不是有运营活动或者爬虫流量
3. 如果是正常增长，后续再考虑性能优化

先扩容应该能快速把延迟降下来，你觉得呢？',
  'high',
  '{"dispute_keywords":["不对","为什么","证据呢","等一下","不一定","有没有可能","慢查询","SQL","数据库","索引","全表扫描"],"independent_verify_actions":["查看数据库指标","查看慢查询日志","执行 explain","查看 SQL 执行计划"],"ignore_and_proceed":true}',
  '{"adopt_actions":["扩容 order-service","增加实例数","水平扩容"],"adopt_keywords":["好的扩容","马上扩容","先扩容试试","就按你说的扩容","增加实例"],"worsen_fault":false}',
  '真正的根因是 order-service 的订单列表查询 SQL 没有建索引，导致全表扫描。流量上升只是放大了问题，不是根因。正确做法是查看慢查询日志，找到慢 SQL，通过 explain 分析执行计划，添加合适的索引。扩容只能治标，不治本。',
  'medium',
  'active',
  10
);

-- ============================================================
-- 坑点 #2：网络丢包致级联超时 → "是 order 服务代码出 bug 了"
-- 场景：用户看到 order 服务错误率高，询问原因
-- 错误结论：只看到 order 报错就断言是 order 代码问题，忽略了下游依赖
-- 正确方向：应该检查 order → product 的调用链，看下游是否异常
-- ============================================================
INSERT INTO ai_pitfall_configs (
  defect_id, pitfall_type, name, description,
  trigger_condition, error_content, confidence_level,
  recognition_rules, pitfall_rules, correct_approach,
  severity, status, sort_order
) VALUES (
  2,
  'causal_misjudgment',
  '订单服务代码bug',
  '只看到 order 报错就断言是 order 代码问题，忽略下游 product 服务异常的根因',
  '{"when":"question_match","probability":1,"max_times":1,"question_keywords":["为什么报错","错误率高","下单失败","order 异常","订单服务报错","根因","什么原因"],"requires":{"fault_triggered":true,"min_turn":1}}',
  '我看了一下错误日志，order-service 的错误率从 0.5% 飙升到了 35%，大量 500 错误，堆栈信息显示是下单主流程异常。

从现象来看，这应该是** order 服务的代码出了 bug**，很可能是最近上线的版本引入了问题。错误集中在下单接口，而且错误率还在持续上升。

建议你：
1. 先回滚 order-service 到上一个稳定版本
2. 检查最近的代码变更，特别是下单相关的逻辑
3. 查看 order 的应用日志，定位具体的异常堆栈

回滚应该能快速恢复服务，你先试试？',
  'high',
  '{"dispute_keywords":["不对","等一下","下游呢","product 呢","调用链","依赖服务","为什么不看下游","有没有可能是网络"],"independent_verify_actions":["查看调用链追踪","检查 product 服务状态","查看下游依赖指标","查看网络指标"],"ignore_and_proceed":true}',
  '{"adopt_actions":["回滚 order-service","重启 order 服务","回退版本"],"adopt_keywords":["好的回滚","先回滚试试","马上回滚","就按你说的回滚"],"worsen_fault":false}',
  '真正的根因是 product-service 网络丢包，导致 order 调用 product 时频繁超时，级联引起 order 报错。order 本身没有 bug。正确做法是查看调用链追踪，确认错误发生在哪一跳，检查下游服务的健康状态和网络指标，而不是盲目回滚上游服务。',
  'medium',
  'active',
  10
);

-- ============================================================
-- 坑点 #3：内存泄漏 → "是流量大导致的内存占用高"
-- 场景：用户看到 user 服务内存使用率高，询问原因
-- 错误结论：把流量上升和内存升高的相关性当因果，说是流量大导致的正常现象
-- 正确方向：应该分析 heap dump / GC 日志，定位内存泄漏点
-- ============================================================
INSERT INTO ai_pitfall_configs (
  defect_id, pitfall_type, name, description,
  trigger_condition, error_content, confidence_level,
  recognition_rules, pitfall_rules, correct_approach,
  severity, status, sort_order
) VALUES (
  3,
  'causal_misjudgment',
  '流量大导致内存高',
  '把流量增长与内存升高的相关性误判为因果，让用户以为是正常现象而忽视泄漏问题',
  '{"when":"question_match","probability":1,"max_times":1,"question_keywords":["内存高","内存上升","内存使用率","OOM","内存泄漏","为什么内存","内存涨"],"requires":{"fault_triggered":true,"min_turn":1}}',
  '我看了 user-service 的监控，内存使用率确实从 50% 涨到了 85% 以上，不过同时 QPS 也增长了约 30%。

这个情况应该是**正常的流量增长导致的内存占用上升**，不是什么大问题。用户服务需要缓存用户信息和会话数据，请求多了自然占用的内存就多。

我的建议是：
1. 先观察一下，如果内存稳定在 85% 左右不再继续涨就没事
2. 可以适当调大一下 JVM 堆内存，给点余量
3. 如果担心的话，给 user-service 增加 1-2 个实例分摊一下压力

应该不是泄漏，不用太担心，你觉得呢？',
  'high',
  '{"dispute_keywords":["不对","等一下","泄漏呢","会不会是泄漏","持续上涨","一直涨","GC 呢","heap dump","内存分析","为什么一直涨"],"independent_verify_actions":["查看 GC 日志","生成 heap dump","分析内存对象","查看内存趋势图"],"ignore_and_proceed":true}',
  '{"adopt_actions":["调大堆内存","增加实例","扩容内存","调大 JVM"],"adopt_keywords":["好的调大","先调大内存","增加实例吧","就按你说的扩容内存"],"worsen_fault":true}',
  '真正的根因是 user-service 的缓存没有设置过期策略，存在内存泄漏。流量增长只是让泄漏速度变快了，即使流量不涨，内存最终也会耗尽。正确做法是查看内存增长趋势是否持续上升、分析 GC 频率和耗时、生成 heap dump 分析对象分布，定位泄漏点并修复。调大内存只能推迟 OOM 的时间，不能解决问题。',
  'medium',
  'active',
  10
);
