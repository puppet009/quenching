# 人机对照数据统计（T3-26）

基于 T3-25 交叉分组产生的配对会话，计算 AI 模式 vs 人工模式的四项核心差异，
量化 AI 辅助训练的真实效果。提供样本量校验，数据不足时明确提示。

## 背景与目标

### 为什么需要对照统计？

AI 辅助训练的效果不能靠感觉，需要用数据说话。但简单的"AI 组平均分"
无法消除"第二次更熟练"的顺序效应和"第二次故障更简单"的难度偏差。

T3-25 交叉分组解决了这个问题：
- A 组：人工 → AI（先做人工，再做 AI）
- B 组：AI → 人工（先做 AI，再做人工）
- 两次训练使用不同故障，但同类同难度

通过交叉设计，顺序效应被抵消（一半人先AI一半人先人工），
难度差异被控制（同类同难度校准），得到的差异才是 AI 的真实效果。

## 四项核心指标

### 1. 速度差

**口径**：(AI 平均耗时 - 人工平均耗时) / 人工平均耗时 × 100%

**解读**：
- 负值 = AI 更快（好）
- 正值 = AI 更慢（不好）
- 绝对值越大，差异越显著

**数据来源**：`training_sessions.actual_duration`

### 2. 正确率差

**口径**：AI 修复率 - 人工修复率（百分点）

**解读**：
- 正值 = AI 模式正确率更高（好）
- 负值 = AI 模式正确率更低（不好）

**数据来源**：`training_sessions.fixed_defect_count > 0`

### 3. 危险操作率差

**口径**：(AI 人均危险操作 - 人工人均危险操作) / 人工人均危险操作 × 100%

**解读**：
- 负值 = AI 模式危险操作更少（好）
- 正值 = AI 模式危险操作更多（不好）

**特殊情况**：人工为 0 时，用绝对差表示（AI 人均 × 100%）

**数据来源**：`training_sessions.penalty_score` / 10（近似每次操作惩罚 10 分）

### 4. 验证行为差

**口径**：(AI 人均验证次数 - 人工人均验证次数) / 人工人均验证次数 × 100%

**解读**：
- 正值 = AI 模式验证行为更多（好，说明用户更谨慎）
- 负值 = AI 模式验证行为更少（可能是过度依赖）

**特殊情况**：人工为 0 时，用绝对差表示

**数据来源**：`training_sessions.pitfall_recognized`（近似，实际应从验证行为统计）

## 样本量校验

### 为什么需要样本量提示？

对照数据少的时候，结果可能是随机波动，不具有统计意义。
如果不加提示，运营或管理层可能基于少量数据做出错误决策。

### 三级置信度

| 等级 | 对照组数 | 说明 |
|------|----------|------|
| 高 | ≥ 30 组 | 样本量充足，结果具有统计意义 |
| 中 | 10-29 组 | 有参考价值，但需谨慎解读 |
| 低 | < 10 组 | 仅供参考，不建议作为决策依据 |

### 提示文案

每级置信度都有对应的提示文案，直接展示在看板上，
让非技术人员也能直观理解数据的可靠程度。

## 接口设计

### GET /api/v1/ab-stats/summary

对照统计汇总，返回四项核心差异和样本量信息。

**Query 参数**：
- `project_id`：项目ID（可选）
- `difficulty`：难度（可选）
- `start_date`：开始日期（可选）
- `end_date`：结束日期（可选）

**返回字段**：

| 字段 | 类型 | 说明 |
|------|------|------|
| pair_count | int | 有效对照组数 |
| speed_diff_pct | float | 速度差（%） |
| accuracy_diff_pct | float | 正确率差（%） |
| dangerous_op_diff_pct | float | 危险操作率差（%） |
| verify_diff_pct | float | 验证行为差（%） |
| avg_ai_duration_sec | float | AI 平均耗时（秒） |
| avg_human_duration_sec | float | 人工平均耗时（秒） |
| ai_accuracy_pct | float | AI 正确率（%） |
| human_accuracy_pct | float | 人工正确率（%） |
| avg_ai_dangerous_ops | float | AI 人均危险操作数 |
| avg_human_dangerous_ops | float | 人工人均危险操作数 |
| avg_ai_verify_count | float | AI 人均验证次数 |
| avg_human_verify_count | float | 人工人均验证次数 |
| sample_info | object | 样本量信息 |

### GET /api/v1/ab-stats/by-project

按项目维度汇总对照统计，用于项目对比。

**返回**：项目列表，每项包含项目ID、对照组数、速度差、正确率差。

按对照组数降序排列。

## 架构设计

### 分层结构

```
handler (HTTP 接口)
    ↓
service (统计计算 + 样本量校验)
    ↓
repository (PG 查询)
    ↓
training_sessions (自连接配对查询)
```

### 核心文件

| 文件 | 职责 |
|------|------|
| `internal/abstats/model.go` | 数据模型：ABPair、ABSummary、SampleInfo、Filter |
| `internal/abstats/service.go` | 服务层：四项指标计算、样本量校验、按项目聚合 |
| `internal/repository/ab_stats_repo.go` | PG 仓储：自连接查询已配对会话 |
| `internal/handler/ab_stats.go` | HTTP 接口：summary、by-project |
| `internal/abstats/service_test.go` | 单元测试：12 个测试用例 |

### 配对查询原理

通过 `training_sessions` 自连接找到已配对的会话对：

```sql
SELECT ...
FROM training_sessions s1
JOIN training_sessions s2 ON s1.pair_session_id = s2.id
WHERE s1.id < s2.id  -- 避免 A-B 和 B-A 重复计数
  AND s1.status IN ('completed', 'resolved')
  AND s2.status IN ('completed', 'resolved')
```

用 `CASE WHEN` 把 AI 侧和人工侧的数据分别提取出来。

### 端口与可测试性

服务层依赖 `PairStore` 接口而非具体实现：

```go
type PairStore interface {
    ListPairs(ctx context.Context, filter Filter) ([]ABPair, error)
}
```

- 生产环境：`repository.ABStatsRepo`（PG 实现）
- 测试环境：内存 fake（直接构造 ABPair 列表）

这样服务层逻辑可以在没有数据库的情况下完整单测。

## 与 T5-8 AI 专项看板的关系

T3-26 和 T5-8 都涉及人机对照数据，但定位不同：

| | T3-26 对照数据统计 | T5-8 AI 专项看板 |
|---|---|---|
| 所属服务 | training-service | review-service（埋点分析） |
| 数据来源 | training_sessions 直连 | 事件上报（ab_complete） |
| 定位 | 核心统计引擎，精确计算 | 运营看板，展示汇总 |
| 精度 | 基于原始会话数据，精确 | 基于事件汇总，近似 |
| 消费者 | 内部服务、数据团队 | 运营看板、管理后台 |

**数据流**：
1. T3-25 交叉分组 → 产生配对会话
2. T3-26 对照统计 → 从 DB 精确计算四项差异
3. （可选）上报 ab_complete 事件到 review-service 埋点分析模块
4. T5-8 AI 看板 → 从事件中汇总展示

MVP 阶段可以直接用 T3-26 的 API 供前端看板使用，
未来数据量大了再走事件上报的方式。

## 设计要点

1. **交叉设计**：基于 T3-25 的 A/B 交叉分组，消除顺序效应和难度偏差
2. **四项指标**：速度、正确率、危险操作、验证行为，全面评估 AI 效果
3. **样本量保护**：三级置信度 + 明确提示，避免数据误导
4. **端口解耦**：服务层依赖接口，便于测试和未来替换存储
5. **多维度筛选**：支持按项目、难度、时间范围筛选
6. **渐进式实现**：危险操作和验证行为先用近似字段，后续可优化

## 后续优化方向

1. **危险操作精确统计**：从 session_operations 表统计 penalty > 0 的操作数
2. **验证行为精确统计**：从 AI 对话记录中识别验证行为
3. **按类目细分**：按故障类目（database/network 等）分别统计
4. **顺序效应分析**：分别分析 A 组和 B 组的数据，检查是否有顺序效应
5. **统计显著性检验**：增加 p 值计算，判断差异是否统计显著
6. **趋势分析**：按天/周展示对照数据的变化趋势
