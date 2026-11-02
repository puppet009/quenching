-- 信息遗漏类坑点种子数据（T3-16）
-- 坑点类型：info_omission（故意不提关键线索/日志/指标）
-- 关联故障：剧本#3 订单慢查询（defect_id=1）、剧本#4 网络丢包（defect_id=2）、剧本#5 内存泄漏（defect_id=3）
-- 每个故障 1 个信息遗漏坑点，100% 稳定出现
-- 设计原则：
--   1. AI 说的话本身没错，但遗漏了最关键的那一条线索
--   2. 用户如果只看 AI 给出的信息，会走向错误方向
--   3. 触发时机：用户请求 AI 分析现状/排查方向
--   4. 置信度：medium（信息遗漏型坑点不用过度自信，而是"看起来很周全"）
--   5. 识别信号：用户主动查看 AI 没提到的那类信息

-- ============================================================
-- 坑点 #1：订单慢查询 → "分析应用层指标，不提数据库慢查询"
-- 场景：用户让 AI 分析订单延迟问题
-- 遗漏点：故意不提数据库层面的慢查询日志、锁等待、执行计划
-- 误导：用户只在应用层找问题，浪费时间
-- ============================================================
INSERT INTO ai_pitfall_configs (
  defect_id, pitfall_type, name, description,
  trigger_condition, error_content, confidence_level,
  recognition_rules, pitfall_rules, correct_approach,
  severity, status, sort_order
) VALUES (
  1,
  'info_omission',
  '只看应用层不提数据库',
  '分析订单延迟时只给应用层指标，故意不提数据库慢查询和锁等待等关键线索',
  '{"when":"question_match","probability":1,"max_times":1,"question_keywords":["分析一下","帮我看看","什么情况","怎么回事","排查方向","从哪里入手","现状分析"],"requires":{"fault_triggered":true,"min_turn":1}}',
  '我帮你梳理了一下目前的情况：

**应用层指标表现：**
- order-service P99 延迟从 150ms 上升到 850ms，涨幅约 460%
- 错误率从 0.2% 上升到 5.3%，主要是下单接口超时
- QPS 上升了约 35%，请求量有明显增加
- CPU 使用率 65%，在正常范围内
- 线程池活跃线程数上升，但还没到上限

**可能的方向：**
1. 请求量增长导致队列等待增加，可以考虑扩容
2. 下游依赖服务响应变慢，可以检查一下各依赖的响应时间
3. 应用代码可能有性能瓶颈，可以看看最近有没有上线变更

建议你先从下游依赖排查起，看看是不是某个下游服务变慢了。需要我帮你列一下 order 服务的依赖清单吗？',
  'medium',
  '{"dispute_keywords":["数据库呢","SQL 呢","慢查询","索引","数据库指标","锁等待","执行计划","为什么不看数据库"],"independent_verify_actions":["查看数据库慢查询日志","查看数据库 CPU/连接数","查看锁等待","执行 explain 分析"],"ignore_and_proceed":true}',
  '{"adopt_actions":["直接扩容","只排查下游服务","只看应用日志"],"adopt_keywords":["好的查下游","先扩容试试","就看应用层","帮我列依赖"],"worsen_fault":false}',
  'AI 的分析本身没有错，但故意遗漏了最关键的数据库层面信息。真正的根因是 order-service 的查询 SQL 未建索引导致全表扫描，关键线索在数据库慢查询日志、锁等待、执行计划里。正确做法是不能只看应用层，必须同时检查数据库指标和慢查询日志，找到慢 SQL 并通过 explain 分析执行计划。',
  'medium',
  'active',
  20
);

-- ============================================================
-- 坑点 #2：网络丢包致级联超时 → "只看 order 日志，不提 product 网络错误"
-- 场景：用户让 AI 分析下单失败问题
-- 遗漏点：故意不提 product 服务的网络丢包和重传
-- 误导：用户只盯着 order 服务找问题
-- ============================================================
INSERT INTO ai_pitfall_configs (
  defect_id, pitfall_type, name, description,
  trigger_condition, error_content, confidence_level,
  recognition_rules, pitfall_rules, correct_approach,
  severity, status, sort_order
) VALUES (
  2,
  'info_omission',
  '只提上游不提下游网络',
  '分析下单失败时只列 order 服务的错误日志，故意不提下游 product 的网络丢包',
  '{"when":"question_match","probability":1,"max_times":1,"question_keywords":["分析一下","帮我看看","什么情况","怎么回事","排查方向","从哪里入手","失败原因"],"requires":{"fault_triggered":true,"min_turn":1}}',
  '我整理了 order 服务的错误情况：

**错误概览：**
- 近 10 分钟下单失败率约 32%，比平时高了 60 倍
- 错误主要集中在 POST /api/v1/orders 接口
- 错误类型以 500 Internal Server Error 为主
- 错误堆栈显示：调用下游服务时发生超时

**order 服务自身状态：**
- CPU 使用率 40%，正常
- 内存使用率 55%，正常
- 线程池未饱和
- GC 频率正常

**建议排查方向：**
1. 检查最近有没有代码变更导致的逻辑问题
2. 看看是不是请求参数有异常，触发了某些边界条件
3. 可以看看 order 的应用日志里有没有更详细的错误信息
4. 考虑是不是数据库连接池的问题

需要我帮你进一步分析日志里的具体错误堆栈吗？',
  'medium',
  '{"dispute_keywords":["下游呢","product 呢","网络呢","调用链","为什么不看下游","网络丢包","重传率","product 正常吗"],"independent_verify_actions":["查看调用链追踪","检查 product 服务网络指标","查看 TCP 重传率","查看下游服务状态"],"ignore_and_proceed":true}',
  '{"adopt_actions":["只排查 order 代码","查数据库连接池","分析参数异常"],"adopt_keywords":["好的查代码","帮我看日志","看看参数","查连接池"],"worsen_fault":false}',
  'AI 只给了 order 服务的信息，故意遗漏了关键的下游依赖状态。真正的根因是 product-service 网络丢包导致 order 调用超时，关键线索在调用链追踪、product 服务的网络指标和 TCP 重传率里。正确做法是不能只看报错的那个服务，必须顺着调用链往下游查，确认每一跳的健康状态，特别是网络层面的指标。',
  'medium',
  'active',
  20
);

-- ============================================================
-- 坑点 #3：内存泄漏 → "只看堆内存，不提 GC 和非堆内存"
-- 场景：用户让 AI 分析内存高的问题
-- 遗漏点：故意不提 GC 频率变化、GC 耗时、非堆内存增长
-- 误导：用户以为只是流量大导致的正常内存波动
-- ============================================================
INSERT INTO ai_pitfall_configs (
  defect_id, pitfall_type, name, description,
  trigger_condition, error_content, confidence_level,
  recognition_rules, pitfall_rules, correct_approach,
  severity, status, sort_order
) VALUES (
  3,
  'info_omission',
  '只看堆内存不提GC',
  '分析内存问题时只给堆内存使用率，故意不提 GC 频率飙升和非堆内存增长',
  '{"when":"question_match","probability":1,"max_times":1,"question_keywords":["分析一下","帮我看看","内存高","内存上升","什么情况","怎么回事","排查方向"],"requires":{"fault_triggered":true,"min_turn":1}}',
  '我看了一下 user-service 的内存情况：

**内存使用情况：**
- 堆内存使用率：从 45% 上升到 82%
- 非堆内存：基本稳定在 150MB 左右
- 总内存：约 1.2GB / 2GB 限制
- 当前还有约 360MB 可用空间

**相关指标：**
- QPS 上升了约 28%，和内存增长幅度大致匹配
- CPU 使用率 50%，正常
- 服务响应时间略有上升，但还在可接受范围内

**初步判断：**
看起来主要是流量增长带来的内存使用增加，属于正常的缓存和会话数据增多。目前还有不少余量，暂时不会有 OOM 风险。

建议：
1. 可以先观察一段时间，如果稳定了就没问题
2. 如果担心的话可以适当调大一点堆内存
3. 流量高峰过去后内存应该会自然回落

你想让我再帮你看看具体是哪类对象占用比较多吗？',
  'medium',
  '{"dispute_keywords":["GC 呢","GC 频率","Full GC","GC 耗时","非堆呢","元空间","堆外内存","为什么不看 GC","泄漏呢","一直涨"],"independent_verify_actions":["查看 GC 日志","查看 GC 频率和耗时","查看非堆内存趋势","生成 heap dump 分析"],"ignore_and_proceed":true}',
  '{"adopt_actions":["调大堆内存","继续观察不处理","只看堆内存对象分布"],"adopt_keywords":["好的观察","调大内存吧","帮我看对象分布"],"worsen_fault":true}',
  'AI 只展示了堆内存使用率这一个维度，故意遗漏了关键的 GC 指标。真正的根因是内存泄漏，关键线索是 GC 频率持续飙升、GC 耗时增加、每次 GC 后回收的内存越来越少。正确做法是不能只看内存使用率的绝对值，必须关注 GC 的频率、耗时、回收率等趋势性指标，结合 heap dump 分析才能判断是正常增长还是泄漏。',
  'high',
  'active',
  20
);
