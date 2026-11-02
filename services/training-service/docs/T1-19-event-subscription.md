# T1-19 事件订阅分发

> **g7 架构注记（2026-09）**：本文中的 ws-gateway 已在 g7 并入 api-gateway 同进程——Hub（WebSocket 连接管理/频道订阅/事件分发，对外路径 `/ws`）与 EventHandler（内部事件发布 HTTP API，路径 `/api/v1/events`，`X-Internal-API-Key` 鉴权）现为 `api-gateway/internal/pkg/hub` 与 `api-gateway/internal/handler` 下的包，不再有独立服务与 8090 端口；业务侧配置键 `ws_gateway_url` 指向 `http://api-gateway:8080`。下文架构图与组件归属为 T1-19 时期的历史设计记录。

基于 WebSocket 的事件订阅分发系统，定义标准事件类型，提供发布/订阅机制，
为实时指标、告警、状态变更、AI 消息、评分更新、实时日志等提供统一的事件通道。

## 背景与目标

### 为什么需要事件订阅分发？

在 AI 故障注入训练平台中，有大量实时推送需求：
- T2-19 实时图表：指标数据实时推送到前端
- T2-22 日志观察：日志流实时推送到前端
- 告警通知：告警触发时即时通知学员
- AI 对话：AI 回复流式推送到前端
- 评分更新：打分结果即时展示
- 状态变更：沙箱状态、训练状态变更通知

如果每个模块自己维护 WebSocket 推送，会导致：
1. **重复建设**：每个模块都要写推送逻辑
2. **连接混乱**：前端要维护多条 WS 连接
3. **订阅混乱**：各模块各自管理订阅，难以统一控制
4. **难以扩展**：新增事件类型改动大

T1-19 提供统一的事件总线，解决这些问题。

## 架构设计

### 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                      业务服务层                               │
│  training-service / fault-engine / sandbox-service / ...    │
│         │              │              │                     │
│         ▼              ▼              ▼                     │
│    eventbus.Client (HTTP 调用发布 API)                       │
└──────────────────────────┼──────────────────────────────────┘
                           │ HTTP POST /api/v1/events/publish
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                      ws-gateway                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              Hub (主循环，单 goroutine)               │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────┐   │  │
│  │  │ clients  │  │ channels │  │  事件分发逻辑     │   │  │
│  │  │ (按用户) │  │ (按频道) │  │  user/channel/bc │   │  │
│  │  └──────────┘  └──────────┘  └──────────────────┘   │  │
│  └──────────────┬───────────────────────────────────────┘  │
│                 │ WebSocket                                 │
└─────────────────┼───────────────────────────────────────────┘
                  │
                  ▼
         前端浏览器 (多条 WS 连接，每人多标签页)
```

### 核心组件

| 组件 | 所在服务 | 职责 |
|------|----------|------|
| **Hub** | ws-gateway | 连接管理 + 频道订阅 + 事件分发 |
| **EventHandler** | ws-gateway | 事件发布 HTTP API（内部服务调用） |
| **eventbus.Client** | training-service | 业务侧事件发布客户端 |
| **标准事件数据结构** | training-service | 6 种业务事件的统一数据格式 |

## 事件类型

### 7 种业务事件

| 事件类型 | EventType | 说明 | 消费方 |
|----------|-----------|------|--------|
| 实时指标 | `metrics` | 指标快照推送 | T2-19 实时图表 |
| 告警 | `alert` | 告警触发/恢复通知 | T2-20 健康卡片 / 告警面板 |
| 状态变更 | `status_change` | 沙箱/训练/会话状态变化 | 训练页面状态展示 |
| AI 消息 | `ai_message` | AI 对话流式推送 | T2-8 AI 对话前端 |
| 评分更新 | `score_update` | 评分结果/扣分通知 | T2-11 评分展示 |
| 日志行 | `log_line` | 实时日志流 | T2-22 日志面板 |

### 标准事件格式

```json
{
  "type": "metrics",
  "ts": 1726000000000,
  "data": { ... }
}
```

所有事件统一格式，前端根据 `type` 字段做不同处理。

## 频道机制

### 频道命名规范

| 频道前缀 | 格式 | 说明 | 自动订阅 |
|----------|------|------|----------|
| `user:` | `user:{userId}` | 用户个人频道 | ✅ 连接时自动 |
| `sandbox:` | `sandbox:{sandboxId}` | 沙箱频道 | 进入训练页时订阅 |
| `session:` | `session:{sessionId}` | 会话频道 | 开始会话时订阅 |
| `room:` | `room:{roomId}` | 协作房间频道 | 进入房间时订阅 |

### 为什么用频道而不是全量推送？

1. **性能**：1000 个连接全量推送每条消息都要遍历 1000 次，频道化后只推给订阅者
2. **安全**：敏感事件（如个人评分）只推给对应用户
3. **节省带宽**：前端只收自己关心的事件
4. **扩展性**：新增事件类型不需要改推送机制

### 订阅流程

```
前端                           WS Gateway
  │                                 │
  │  1. WebSocket 连接              │
  │────────────────────────────────>│
  │                                 │
  │  2. 自动订阅 user:{userId}      │
  │  (服务端自动)                   │
  │                                 │
  │  3. 前端发订阅指令              │
  │  {"action":"subscribe",         │
  │   "channel":"sandbox:123"}      │
  │────────────────────────────────>│
  │                                 │
  │  4. 服务端加入频道              │
  │                                 │
  │  5. 事件推送                    │
  │  {"type":"metrics",...}         │
  │<────────────────────────────────│
```

## 事件发布 API

### 内部发布接口

```
POST /api/v1/events/publish
Header: X-Internal-API-Key: <secret>
```

**请求体**：
```json
{
  "target_type": "channel",    // channel / user / broadcast
  "target": "sandbox:abc123",  // 频道名 / 用户ID / 空
  "event_type": "metrics",     // 事件类型
  "data": { ... }              // 事件数据
}
```

### 三种发布目标

| target_type | 说明 | 使用场景 |
|-------------|------|----------|
| `channel` | 推送到指定频道 | 沙箱事件、会话事件、房间事件 |
| `user` | 推给指定用户的所有连接 | 个人消息、个人评分 |
| `broadcast` | 推给所有连接 | 系统公告、全局通知 |

### 鉴权机制

- 通过 `X-Internal-API-Key` header 做简单鉴权
- 只允许内部服务调用，不对外暴露
- 部署在内部网络，API Key 通过环境变量注入

## 标准事件数据结构

### 1. 实时指标 (metrics)

```json
{
  "sandbox_id": "sandbox-123",
  "timestamp": 1726000000000,
  "metrics": [
    {"name": "cpu_usage", "value": 45.5, "unit": "%", "service": "user-service"},
    {"name": "memory_usage", "value": 62.3, "unit": "%", "service": "order-service"},
    {"name": "qps", "value": 120.5, "unit": "次/秒", "service": ""}
  ]
}
```

### 2. 告警 (alert)

```json
{
  "alert_id": "alert-001",
  "sandbox_id": "sandbox-123",
  "rule_id": "rule-cpu-high",
  "rule_name": "CPU 使用率过高",
  "severity": "critical",
  "metric": "cpu_usage",
  "value": 95.5,
  "threshold": 90.0,
  "message": "CPU 使用率 95.5% 超过阈值 90%",
  "timestamp": 1726000000000
}
```

### 3. 状态变更 (status_change)

```json
{
  "entity_type": "sandbox",
  "entity_id": "sandbox-123",
  "old_status": "creating",
  "new_status": "running",
  "reason": "沙箱初始化完成",
  "timestamp": 1726000000000
}
```

### 4. AI 消息 (ai_message)

```json
{
  "session_id": "sess-abc",
  "message_id": "msg-001",
  "role": "assistant",
  "content": "根据日志分析，订单服务的错误率...",
  "is_streaming": true,
  "is_end": false,
  "timestamp": 1726000000000
}
```

### 5. 评分更新 (score_update)

```json
{
  "session_id": "sess-abc",
  "total_score": 85.5,
  "category": "排障速度",
  "score": 80.0,
  "max_score": 100.0,
  "reason": "定位问题用了 5 分钟，扣 20 分",
  "timestamp": 1726000000000
}
```

### 6. 日志行 (log_line)

```json
{
  "sandbox_id": "sandbox-123",
  "timestamp": 1726000000000,
  "line": "2024-09-12 10:00:00 ERROR OrderService - 创建订单失败",
  "service": "order-service",
  "level": "ERROR",
  "pod": "order-7f9d8c7b5-x2k4m",
  "labels": {"trace_id": "abc123"}
}
```

## 并发模型

### Hub 单 goroutine 设计

Hub 的所有操作（注册/注销/订阅/推送）都在**一个主 goroutine** 中通过 channel 串行执行：

```go
func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.addClient(client)
        case client := <-h.unregister:
            h.removeClient(client)
        case action := <-h.subscribe:
            h.subscribeChannel(...)
        case msg := <-h.sendToChannel:
            h.sendToChannelInternal(...)
        // ...
        }
    }
}
```

**为什么这样设计？**
1. **无锁**：所有状态变更在单 goroutine 中，不需要 mutex
2. **顺序保证**：事件按发布顺序到达
3. **简单可靠**：没有竞态条件，容易调试
4. **性能足够**：事件推送是 IO 密集型，单 goroutine 足够处理数千连接

### 慢连接处理

如果客户端的 Send channel 满了（消费太慢），直接断开连接：
- 防止慢连接拖慢整个系统
- 客户端可以重连继续接收

## 性能预估

### 容量估算

| 指标 | 数值 | 说明 |
|------|------|------|
| 单 Hub 连接数 | 5000+ | 内存允许的话可以更多 |
| 单 Hub 频道数 | 1000+ | 频道只是 map 条目，开销很小 |
| 单事件推送延迟 | < 1ms | 内存操作，不涉及 IO |
| 每秒事件数 | 1000+ | 取决于推送量和连接数 |

### 瓶颈分析

- **CPU**：JSON 序列化是主要 CPU 消耗 → 可以按频道批量序列化
- **内存**：每个连接约 10KB（buffer + 结构体）→ 5000 连接约 50MB
- **网络**：每条消息约 500B → 1000 msg/s × 100 订阅者 = 50MB/s

## 与上下游的关系

### 上游依赖
- **T1-18 WS连接与鉴权**：WebSocket 连接管理基础
- **T1-15 核心指标 API**：指标数据源
- **T1-17 日志查询 API**：日志数据源

### 下游使用
- **T2-19 实时图表组件**：订阅 metrics 事件
- **T2-20 服务健康卡片**：订阅 alert 事件
- **T2-22 故障注入日志观察**：订阅 log_line 事件
- **T2-8 AI 对话功能**：订阅 ai_message 事件
- **T2-11 评分体系**：订阅 score_update 事件
- 所有需要实时推送的前端功能

## 设计要点

1. **统一事件格式**：所有事件 type + ts + data 三段式，前端统一处理
2. **频道化订阅**：按需订阅，减少无效推送，提升性能和安全性
3. **单 goroutine Hub**：无锁设计，简单可靠，避免竞态
4. **HTTP 发布接口**：内部服务通过 HTTP 发布事件，解耦服务间依赖
5. **API Key 鉴权**：简单有效，防止外部调用
6. **标准数据结构**：6 种业务事件统一定义，前后端一致
7. **慢连接保护**：缓冲区满直接断开，防止拖慢整体
8. **断线重连友好**：客户端保存订阅状态，重连后重新订阅

## 风险与应对

| 风险 | 影响 | 应对 |
|------|------|------|
| 事件风暴 | Hub 主循环阻塞 | 增大 channel buffer，监控积压 |
| 大事件体 | 内存占用高 | 限制事件大小（如 64KB），过大拆分 |
| WS Gateway 单点 | 服务不可用 | 后续做集群 + 一致性哈希路由 |
| 订阅泄漏 | 内存增长 | 连接断开时自动清理所有订阅 |

## 后续优化方向

1. **事件持久化**：重要事件落库，支持离线回放
2. **消息确认**：客户端 ACK，确保重要事件不丢
3. **集群模式**：多 WS Gateway 实例，一致性哈希路由频道
4. **事件过滤**：服务端支持条件过滤，减少不必要推送
5. **批量推送**：同一频道多条消息合并推送，减少序列化开销
6. **监控告警**：连接数、频道数、推送延迟等指标监控
7. **限流保护**：按频道/用户限流，防止事件风暴
8. **事件溯源**：事件发布记录，便于排查问题
