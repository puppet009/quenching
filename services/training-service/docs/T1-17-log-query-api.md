# T1-17 日志查询 API

封装 Loki 的 HTTP 查询接口，提供业务友好的日志查询 API，供训练界面日志面板、管理后台、复盘报告等模块使用。

## 背景与目标

### 为什么需要统一的日志查询层？

T1-16 已经把日志采集到平台侧 Loki 了，但直接让前端调用 Loki API 有问题：
1. **安全风险**：Loki 原生 API 权限粒度粗，前端直连可能越权查别人沙箱的日志
2. **接口不友好**：Loki 的 LogQL 语法复杂，前端直接写查询容易出错
3. **难以复用**：多个模块（日志面板、复盘、告警）都要查日志，各自写一遍重复劳动
4. **不易测试**：每个模块单独对接 Loki，测试成本高

T1-17 提供统一的日志查询服务层，解决这些问题。

## 架构设计

### 分层结构

```
handler (HTTP API, 5个端点)
    ↓
service (业务查询 + 统计 + Tail)
    ↓
LogQL 构建器（组装查询表达式）
    ↓
LokiClient (Loki HTTP API 封装)
    ↓
Loki (平台侧)
```

### 核心文件

| 文件 | 职责 |
|------|------|
| `internal/logquery/loki_client.go` | Loki HTTP 客户端（QueryRange/Query/Count） |
| `internal/logquery/service.go` | 日志查询服务 + 统计 + Tail |
| `internal/handler/logs.go` | HTTP 接口（5 个端点） |
| `internal/logquery/service_test.go` | 单元测试 |

## API 设计

### 5 个端点

| 方法 | 路径 | 说明 | 典型场景 |
|------|------|------|----------|
| GET | `/api/v1/logs/query` | 分页查询日志 | 历史日志翻页、按条件过滤 |
| GET | `/api/v1/logs/latest` | 最新 N 条日志 | 日志面板初始加载 |
| GET | `/api/v1/logs/stats` | 日志统计信息 | 日志概览卡片、错误数趋势 |
| GET | `/api/v1/logs/tail` | 实时日志（轮询式） | 实时日志流、WebSocket 推送 |
| GET | `/api/v1/logs/services` | 可用服务列表 | 服务下拉选项 |

### 查询参数

公共参数：
- `sandbox_id`：沙箱 ID（**必填**，权限隔离的关键）
- `service`：服务名（可选，空=所有服务）
- `level`：日志级别（可选：DEBUG/INFO/WARN/ERROR/FATAL）
- `keyword`：关键词过滤（可选，grep 式匹配）

区间查询额外参数：
- `start_time`：开始时间（毫秒时间戳）
- `end_time`：结束时间（毫秒时间戳）
- `limit`：返回条数上限，默认 200
- `direction`：方向（backward 从新到旧 / forward 从旧到新）

### 响应格式

```json
{
  "code": 0,
  "msg": "ok",
  "data": {
    "total": 200,
    "has_more": true,
    "services": ["user-service", "order-service"],
    "entries": [
      {
        "timestamp": 1726000000000,
        "line": "2024-09-12 10:00:00 ERROR OrderService - 创建订单失败...",
        "labels": {
          "service": "order-service",
          "level": "ERROR",
          "pod": "ecom-order-service-7f9d8c7b5-x2k4m"
        }
      }
    ]
  }
}
```

### 统计接口响应

```json
{
  "code": 0,
  "msg": "ok",
  "data": {
    "total_count": 5280,
    "error_count": 12,
    "warn_count": 85,
    "info_count": 5183,
    "by_service": {
      "user-service": 2100,
      "order-service": 1800,
      "product-service": 1380
    },
    "time_range": "14:00:00 ~ 15:00:00"
  }
}
```

## LogQL 构建规则

### 基础查询格式

```logql
{sandbox_id="xxx", service="yyy", level="ZZZ"} |= "keyword"
```

### 构建顺序

1. **必填**：`sandbox_id` —— 保证沙箱隔离
2. **可选**：`service` —— 按服务过滤
3. **可选**：`level` —— 按级别过滤
4. **可选**：`|= "keyword"` —— 关键词过滤（管道操作符）

### 为什么用 |= 而不是 |~？

- `|=`：字符串包含匹配，性能好，足够用
- `|~`：正则匹配，性能差，容易写错

大部分场景用字符串匹配就够了，正则匹配留给高级用法（后续扩展）。

## 实时日志方案

### 两种实现方式对比

| 方案 | 优点 | 缺点 |
|------|------|------|
| **轮询式 Tail**（本方案） | 实现简单、稳定、不依赖长连接 | 有延迟（取决于轮询间隔） |
| Loki WebSocket | 实时性好 | Loki 2.x 才支持、复杂度高 |

### 当前方案：轮询式 Tail

- 前端每 2-3 秒调用一次 `/api/v1/logs/tail?since=xxx`
- `since` 参数传上次最后一条日志的时间戳
- 服务端用 `direction=forward` 返回新日志（从旧到新，便于前端追加）
- 前端拿到后追加到列表底部

**后续优化**：T1-19 事件订阅分发上线后，可以改成 WebSocket 推送模式，由后端主动推新日志。

## 权限隔离

### 关键：sandbox_id 强制校验

所有日志查询接口都**强制要求** `sandbox_id` 参数，并且：
1. 训练服务内部校验用户是否有该沙箱的访问权限
2. LogQL 查询必须带 `sandbox_id` label 过滤
3. 禁止不带 `sandbox_id` 的全量查询

### 为什么不在 Loki 层做多租户？

Loki 支持多租户（通过 X-Scope-OrgID header），但：
1. 每沙箱一个租户 → 租户数量大，Loki 管理复杂
2. 我们的查询入口统一在训练服务，在这里做权限校验更简单
3. 单租户 Loki 运维成本低

## 性能考量

### 1. Label 限制

严格使用 T1-16 定义的 5 个 label：
- `sandbox_id`、`namespace`、`service`、`level`、`pod`
- 禁止把 trace_id、request_id 等高基数字段放 label

### 2. 查询限制

- `limit` 默认 200，最大 1000
- 默认时间范围 1 小时，避免大范围查询
- 统计接口并发查询 4 个指标，减少总耗时

### 3. 后续优化方向

- **查询缓存**：相同查询短时间内缓存结果（如 5 秒）
- **查询限流**：每沙箱每秒最多 N 次查询
- **异步导出**：大范围日志导出走异步任务，不阻塞 API

## 与上下游的关系

### 上游依赖
- **T1-16 日志采集到 Loki**：提供原始日志数据
- Loki 服务（平台侧部署）

### 下游使用
- **T1-22 故障注入日志观察**：训练界面的日志面板
- **T2-15 指标轮询采集**：恢复判定可结合日志关键词
- **T4-11 沙箱监控后端**：管理后台查看沙箱日志
- **复盘报告**：训练结束后拉取关键日志片段
- **告警模块**：基于日志关键字的告警规则

### 数据流向

```
业务服务 stdout
    ↓
Promtail → Loki (平台侧)
    ↓
logquery.Service (T1-17)
    ↓
HTTP API ─┬─→ 训练界面日志面板 (T1-22)
          ├─→ 管理后台 (T4-11)
          ├─→ 复盘报告
          └─→ 告警判定
```

## 设计要点

1. **沙箱隔离**：所有查询强制带 sandbox_id，杜绝越权
2. **接口友好**：封装 LogQL，前端不用学 Loki 查询语法
3. **实时可用**：提供 tail 接口，支持轮询式实时日志
4. **统计便捷**：并发查询各级别数量，一次返回统计信息
5. **分页友好**：has_more 标记，前端翻页体验好
6. **可扩展**：LokiClient 是接口，方便 mock 测试和未来替换
7. **容错设计**：统计接口单个查询失败不影响整体结果

## 后续优化方向

1. **WebSocket 实时推送**：结合 T1-19 事件总线，主动推新日志
2. **日志高亮**：服务端预处理日志，返回结构化字段（时间/级别/线程/类名）
3. **日志上下文**：点击某条日志，查看前后 N 条上下文
4. **日志导出**：按时间范围导出为 txt/json 文件
5. **高级搜索**：支持正则匹配（`|~`）、排除匹配（`!=`）
6. **日志聚类**：相似日志自动聚类，减少阅读量
7. **异常检测**：基于日志模式的异常检测（如 ERROR 突增）
8. **多沙箱对比**：管理后台对比多个沙箱的日志模式差异
