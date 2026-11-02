# 训练漏斗埋点（T4-13）

事件采集 + 漏斗分析 + 看板统计，为运营数据看板提供数据支撑。

## 交付物

```
埋点分析能力已并入 review-service（原独立的 analytics-service 冻结后删除），
代码位于 review-service 对应分层：

```
services/review-service/
├── internal/
│   ├── model/
│   │   ├── event.go       # 事件模型 + 事件类型常量 + 唯一事件机制
│   │   └── funnel.go      # 漏斗定义 + 漏斗结果结构
│   ├── repository/
│   │   ├── event_repo.go        # EventRepository 接口
│   │   ├── pg_event.go          # PostgreSQL 实现（生产，默认装配）
│   │   └── inmemory_event.go    # 内存版实现（单测）
│   ├── service/
│   │   └── analytics.go         # AnalyticsService（采集+漏斗+看板）
│   └── handler/
│       ├── analytics.go         # 13 个 HTTP 接口
│       └── internal_analytics.go # 服务端上报内部接口（X-Internal-Token）
├── migrations/
│   ├── 000004_create_events.up.sql
│   └── 000004_create_events.down.sql
└── tests/
    └── analytics_test.go        # 50+ 测试用例
```

## 13 个接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/events/track` | 埋点上报 |
| GET | `/api/v1/events` | 事件列表（分页+筛选） |
| GET | `/api/v1/funnels` | 漏斗列表 |
| GET | `/api/v1/funnels/:code/analyze` | 漏斗分析 |
| GET | `/api/v1/dashboard/stats` | 看板统计概览 |

## 事件类型

### 核心漏斗事件（4 步）

| 事件类型 | 说明 | 唯一事件 |
|----------|------|---------|
| register | 用户注册 | ✅ 每人一次 |
| first_start | 首次启动沙箱 | ✅ 每人一次 |
| first_complete | 首次训练完成 | ✅ 每人一次 |
| retrain | 复训（第二次及以后） | ❌ 每次都记 |

### 其他可扩展事件

login, view_project, start_training, submit_fix, use_hint, abandon ...

### 唯一事件机制

- 注册为唯一事件的类型，每个用户只记录第一次
- 通过 `uniqueEvents` map 管理，可运行时注册新类型
- 数据库层用部分唯一索引保证幂等
- 重复上报返回 `recorded: false`，不报错

## 训练漏斗分析

### 漏斗步骤

```
注册 (register) → 首次启动 (first_start) → 首次完成 (first_complete) → 复训 (retrain)
```

### 输出字段

| 字段 | 说明 |
|------|------|
| TotalUsers | 第一步用户数 |
| FinalUsers | 最后一步用户数 |
| OverallConversion | 整体转化率（%） |
| Steps[].UserCount | 该步用户数 |
| Steps[].Conversion | 相对上一步转化率（%） |
| Steps[].DropOff | 相对上一步流失率（%） |

### 计算逻辑

1. 每一步获取该事件的独立用户集合
2. 转化率 = 当前步用户数 / 上一步用户数 × 100%
3. 流失率 = 100% - 转化率
4. 整体转化率 = 最后一步 / 第一步 × 100%

## 看板统计

`/dashboard/stats` 接口返回：

| 字段 | 说明 |
|------|------|
| total_users | 总注册用户数 |
| active_users | 活跃用户（启动过沙箱） |
| completed_users | 完成过训练的用户 |
| retrain_users | 复训用户数 |
| overall_conversion | 注册→完成的整体转化率 |
| training_funnel | 完整漏斗数据 |

## 埋点上报方式

### 前端直接上报

```js
fetch('/api/v1/events/track', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-User-ID': '123' },
  body: JSON.stringify({
    event_type: 'first_start',
    user_id: 123,
    session_id: 456,
    project_id: 1,
    properties: { source: 'homepage' }
  })
})
```

### 后端服务间调用（已接入）

各服务通过 go-common 的 `analytics.Client` 东西向直连内部接口上报，不经网关：

```
POST http://review-service:8087/internal/v1/analytics/events
X-Internal-Token: <internal.token>
```

- 异步发送、失败静默，不影响主业务链路；
- `review_service.base_url` 为空时客户端禁用（本地未起 review-service 也能跑通）。

已接入节点：

| 服务 | 接入点 | 事件 |
|------|--------|------|
| user-service | 注册成功 | `register` |
| sandbox-service | 沙箱进入 ready | `first_start` |
| training-service | 训练完成判定 | `first_complete` |

**首次/复训的判定不在生产端**：生产端统一上报 `first_complete`，
若该唯一事件已存在，review-service 自动补记一条 `retrain`，
保证漏斗第四步无需各服务重复实现「是否首次」的查询逻辑。

### 网关路由

`/api/v1/events`、`/api/v1/funnels`、`/api/v1/dashboard`、`/api/v1/ai-behavior`、
`/api/v1/ai-board` 由 api-gateway 代理到 review-service:8087 并要求 JWT；
`/api/v1/events/publish|stats` 仍由网关进程内 EventHandler 处理（内部 API Key 鉴权）。

## 架构设计

### 存储抽象

EventRepository 接口：
- PG 实现（生产默认，main.go 装配）：落 `events` 表，重启不丢数据、支持多实例
- 内存实现（单测）：零依赖，线程安全

PG 实现要点：
- 写入 `ON CONFLICT DO NOTHING` + `RowsAffected`，重复唯一事件回 `recorded=false`
- 数值属性统计先用 `jsonb_typeof(...)='number'` 过滤，避免 `::numeric` 转换报错
- 属性比较运算符走白名单拼接，防 SQL 注入

### 事件属性（Properties）

使用 JSONB 存储，灵活扩展：
- 无需改表结构即可新增属性
- 支持 GIN 索引，可按属性查询
- 适合埋点这种 schema-less 的数据

### 性能考虑

1. **唯一事件去重**：用部分唯一索引，写入时自动去重
2. **时间范围查询**：`(event_type, occurred_at)` 联合索引
3. **大数据量**：未来可考虑：
   - 按时间分区表（按月/按周）
   - 冷热数据分离
   - 接入 ClickHouse 等 OLAP 数据库

## 安全考虑

1. **埋点接口不做强鉴权**：允许匿名事件（但尽量关联用户ID）
2. **防刷**：可配合 API 网关做限流
3. **IP 记录**：记录客户端 IP，用于异常检测
4. **数据脱敏**：properties 中不应包含敏感信息

## 未来扩展

- **更多漏斗**：注册→登录、启动→完成、首次完成→复购 等
- **留存分析**：次日留存、7日留存、30日留存
- **事件分析**：事件趋势、用户行为路径
- **分群分析**：按用户属性分群对比
- **实时看板**：WebSocket 推送实时数据
- **A/B 实验**：实验分组 + 效果对比
- **数据导出**：CSV/Excel 导出

---

# AI 行为埋点（T4-14）

在 T4-13 基础上扩展 AI 相关行为事件采集和统计分析。

## 新增能力

### 11 种 AI 行为事件

| 分类 | 事件类型 | 说明 |
|------|----------|------|
| 复盘 | review_view | 查看复盘报告 |
| 复盘 | review_share | 分享复盘 |
| 协作评估 | collab_view | 查看协作评估板块 |
| 协作评估 | collab_evaluate | 提交协作评估 |
| 协作评估 | collab_comment | 协作评论 |
| 坑点 | pitfall_encounter | 遇到坑点（触发） |
| 坑点 | pitfall_recognize | 识别坑点 |
| 坑点 | pitfall_fall | 踩坑（受影响） |
| AI 对话 | ai_chat_message | AI 对话消息 |
| AI 对话 | ai_hint_request | AI 提示请求 |
| AI 对话 | ai_explanation | AI 讲解请求 |

### 3 个新增接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/ai-behavior/stats` | AI 行为统计概览 |
| GET | `/api/v1/ai-behavior/breakdown` | AI 事件分类明细 |
| GET | `/api/v1/ai-behavior/pitfall-conversion` | 坑点转化漏斗 |

### 统计概览字段

`/ai-behavior/stats` 返回：

| 字段 | 说明 |
|------|------|
| review_views | 复盘查看次数 |
| review_shares | 复盘分享次数 |
| review_view_users | 查看复盘的独立用户数 |
| collab_views | 协作评估查看次数 |
| collab_evaluates | 协作评估提交数 |
| collab_comments | 协作评论数 |
| collab_view_users | 查看协作评估的独立用户数 |
| pitfall_encounters | 遇到坑点次数 |
| pitfall_recognizes | 识别坑点次数 |
| pitfall_falls | 踩坑次数 |
| pitfall_users | 受坑点影响的独立用户数 |
| ai_chat_messages | AI 对话消息数 |
| ai_hint_requests | AI 提示请求数 |
| ai_explanations | AI 讲解请求数 |
| ai_active_users | 使用 AI 功能的独立用户数 |
| total_ai_events | AI 相关事件总数 |

### 坑点转化漏斗

`/ai-behavior/pitfall-conversion` 返回坑点三步转化：

```
遇到坑点 → 识别坑点 → 踩坑
```

| 字段 | 说明 |
|------|------|
| encounter_users | 遇到坑点用户数 |
| recognize_users | 识别坑点用户数 |
| fall_users | 踩坑用户数 |
| recognition_rate | 识别率（%） |
| fall_rate | 踩坑率（%） |

用于评估坑点教学效果：识别率越高，说明用户对坑点越敏感。

### 事件分类明细

`/ai-behavior/breakdown` 返回所有 AI 事件的明细：
- 事件类型 + 中文名称
- 触发次数
- 独立用户数
- 总数

用于运营看板的事件分布图表。

## 接入方式

各服务在关键节点调用埋点接口上报 AI 事件：

| 服务 | 接入点 | 事件 |
|------|--------|------|
| review-service | 复盘生成后用户查看 | review_view |
| training-service | 协作评估模块查看/提交/评论 | collab_view/evaluate/comment |
| training-service | 坑点引擎触发/识别/踩坑 | pitfall_encounter/recognize/fall |
| ai-service | AI 对话/提示/讲解 | ai_chat_message/hint_request/explanation |

## 设计要点

1. **事件属性灵活扩展**：每个事件都可以带自定义 properties
2. **非唯一事件**：所有 AI 行为事件都是可重复的，每次触发都记录
3. **用户维度统计**：同时统计次数和独立用户数，支持多角度分析
4. **坑点漏斗**：专门的坑点转化漏斗，评估坑点教学效果

---

# 排训指标看板（T5-7）

在 T4-13/T4-14 埋点基础上，构建运营数据看板，提供排训成功指标。

## 核心指标

### 转化类

| 指标 | 口径 | 说明 |
|------|------|------|
| 首训完成率 | 首次完成用户数 / 首次启动用户数 | 衡量新用户转化效果 |
| 复训率 | 复训用户数 / 首次完成用户数 | 衡量产品粘性 |
| 整体完成率 | 总完成场次 / 总训练场次 | 衡量训练成功率 |

### 质量类

| 指标 | 口径 | 说明 |
|------|------|------|
| 复盘好评率 | 评分>=4的评价数 / 总评价数 | 衡量复盘质量和用户满意度 |

### 效率类

| 指标 | 口径 | 说明 |
|------|------|------|
| 平均训练时长 | 所有完成训练的时长平均值 | 衡量训练效率，单位：秒/分钟 |

## 新增事件

| 事件类型 | 说明 | 关键属性 |
|----------|------|----------|
| `training_complete` | 训练完成（每次） | `duration_seconds`、`score`、`difficulty` |
| `review_rating` | 复盘评价 | `score`(1-5)、`review_id`、`comment` |

## 新增接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/dashboard/training-metrics` | 排训核心指标（4大类12项） |
| GET | `/api/v1/dashboard/trends` | 指标日趋势（按天数据点） |
| GET | `/api/v1/dashboard/project-rankings` | 项目排行榜（Top N） |

### 排训核心指标字段

`/dashboard/training-metrics` 返回：

| 字段 | 类型 | 说明 |
|------|------|------|
| first_start_users | int | 首次启动用户数 |
| first_complete_users | int | 首次完成用户数 |
| first_completion_rate | float | 首训完成率（%） |
| retrain_users | int | 复训用户数 |
| retrain_rate | float | 复训率（%） |
| review_ratings | int | 复盘评价总数 |
| positive_ratings | int | 好评数（score >= 4） |
| review_satisfaction | float | 复盘好评率（%） |
| avg_training_duration | float | 平均训练时长（秒） |
| avg_training_duration_min | float | 平均训练时长（分钟） |
| total_sessions | int | 总训练场次 |
| total_completed | int | 总完成场次 |
| completion_rate | float | 整体完成率（%） |

### 日趋势

`/dashboard/trends` 返回每天的数据点数组，用于绘制趋势图：
- 日期、首启用户、首完用户、完成率、训练场次、平均时长

### 项目排行榜

`/dashboard/project-rankings` 返回按启动用户数排序的项目列表：
- 项目ID、启动用户数、完成用户数、完成率、平均时长
- 支持 `top` 参数控制返回数量

## 仓储层扩展

新增 2 个仓储方法，支持基于事件属性的统计：

| 方法 | 说明 |
|------|------|
| `GetAverageNumericProperty` | 获取某事件指定数值属性的平均值 |
| `CountByPropertyValue` | 按属性值筛选统计事件数（eq/ne/gt/gte/lt/lte） |

设计思路：
- 利用 JSONB 字段的灵活性，不需要改表结构即可新增统计维度
- 内存版实现用遍历方式，PG 版可用 JSONB 索引 + `->>` 操作符优化
- 支持 6 种比较运算符，覆盖常见筛选场景

## 接入方式

| 服务 | 接入点 | 事件 |
|------|--------|------|
| training-service | 训练结束判定时 | training_complete（带时长） |
| review-service | 复盘后用户评价 | review_rating（带评分） |

## 设计要点

1. **口径统一**：所有指标有明确的计算口径，避免歧义
2. **多维度看板**：转化、质量、效率、总量四维，全面反映排训效果
3. **趋势分析**：按天粒度的趋势数据，支持时间序列图表
4. **项目维度**：按项目拆解，可对比不同项目的表现
5. **属性统计**：基于 JSONB 属性的灵活统计，不需要新增字段
6. **可扩展**：未来可轻松新增 NPS、留存率等指标

---

# AI 专项看板（T5-8）

在 T4-14 AI 行为埋点基础上，构建 AI 专项数据看板，评估 AI 辅助训练的效果。

## 核心指标

### 坑点识别

| 指标 | 口径 | 说明 |
|------|------|------|
| 识别率 | 识别坑点用户数 / 遇到坑点用户数 | 衡量用户对 AI 坑点的识别能力 |

### 证据验证

| 指标 | 口径 | 说明 |
|------|------|------|
| 验证率 | 有验证行为的用户数 / AI 活跃用户数 | 衡量用户是否习惯验证 AI 结论 |

### 协作查看

| 指标 | 口径 | 说明 |
|------|------|------|
| 协作查看率 | 查看协作评估的用户数 / 完成训练用户数 | 衡量用户对协作评估板块的关注度 |

### 危险操作

| 指标 | 口径 | 说明 |
|------|------|------|
| 人均危险操作数 | 危险操作总数 / AI 活跃用户数 | 衡量 AI 模式下用户的操作安全性 |

### 人机对照 A/B

| 指标 | 说明 |
|------|------|
| 速度差 | AI 模式 vs 人工模式的速度差异（%） |
| 正确率差 | AI 模式 vs 人工模式的正确率差异（%） |
| 危险操作率差 | AI 模式 vs 人工模式的危险操作率差异（%） |
| 验证行为差 | AI 模式 vs 人工模式的验证行为率差异（%） |

## 样本量提示

为避免数据误导，看板提供明确的样本量提示：

| 维度 | 充足阈值 | 不足提示 |
|------|----------|----------|
| 用户数 | ≥ 50 | "样本量不足（<50用户），指标仅供参考" |
| 对照数 | ≥ 30 | "对照样本不足（<30组），A/B 差异仅供参考" |

**置信度等级**：
- **高**：用户数和对照数都充足
- **中**：一个充足，一个不足
- **低**：都不足

## 新增事件

| 事件类型 | 说明 | 关键属性 |
|----------|------|----------|
| `evidence_verify` | 证据验证行为 | `verify_type`（log_check/metric_check/experiment） |
| `dangerous_op` | 危险操作 | `op_type`、`severity`、`cost` |
| `ab_complete` | 完成一组对照训练 | `speed_diff_pct`、`accuracy_diff`、`dangerous_op_diff_pct`、`verify_diff_pct`、`group` |

## 新增接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/ai-board/metrics` | AI 专项核心指标 |
| GET | `/api/v1/ai-board/feature-breakdown` | AI 功能使用明细 |

### AI 专项核心指标字段

`/ai-board/metrics` 返回：

| 字段 | 类型 | 说明 |
|------|------|------|
| pitfall_encounter_users | int | 遇到坑点用户数 |
| pitfall_recognize_users | int | 识别坑点用户数 |
| recognition_rate | float | 识别率（%） |
| ai_active_users | int | AI 活跃用户数 |
| verify_users | int | 有验证行为的用户数 |
| verification_rate | float | 验证率（%） |
| completed_users | int | 完成训练用户数 |
| collab_view_users | int | 查看协作评估的用户数 |
| collab_view_rate | float | 协作查看率（%） |
| total_dangerous_ops | int | 危险操作总数 |
| avg_dangerous_ops_per_user | float | 人均危险操作数 |
| ab_pair_count | int | 完成的对照组数 |
| ab_speed_diff | float | 速度差（%，AI-人工） |
| ab_accuracy_diff | float | 正确率差（%，AI-人工） |
| ab_dangerous_op_diff | float | 危险操作率差（%，AI-人工） |
| ab_verify_diff | float | 验证行为差（%，AI-人工） |
| sample_info | object | 样本量信息 |

### 样本量信息字段

| 字段 | 类型 | 说明 |
|------|------|------|
| total_users | int | 总用户数 |
| ab_pair_count | int | 对照组数 |
| enough_users | bool | 用户数是否足够 |
| enough_ab | bool | 对照数是否足够 |
| user_tip | string | 用户数提示文案 |
| ab_tip | string | 对照数提示文案 |
| overall_confidence | string | 整体置信度（高/中/低） |

### AI 功能使用明细

`/ai-board/feature-breakdown` 返回按功能分类的使用情况：

- 分类：AI对话 / 复盘 / 坑点 / 协作 / 验证
- 每个功能：用户数、总次数、使用率（相对AI活跃用户）

用于绘制功能使用分布图表。

## 接入方式

| 服务 | 接入点 | 事件 |
|------|--------|------|
| training-service | 操作代价计分模块 | dangerous_op |
| training-service | 证据验证率模块（T3-23） | evidence_verify |
| training-service | 人机对照统计（T3-26） | ab_complete |

## 设计要点

1. **样本量保护**：数据不足时明确提示，避免误导决策
2. **多维度评估**：从识别、验证、协作、安全四个维度评估 AI 效果
3. **对照分析**：A/B 对照数据量化 AI 的真实价值
4. **功能拆解**：按功能维度拆解使用情况，可定位薄弱环节
5. **置信度分级**：高/中/低三级置信度，直观反映数据可靠性

