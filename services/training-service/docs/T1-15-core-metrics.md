# T1-15 核心指标定义与查询 API

定义 5 大核心监控指标的口径和 PromQL 表达式，封装 Prometheus 查询为统一的 Go API 和 HTTP 接口，供恢复判定、实时图表、告警等模块使用。

## 背景与目标

### 为什么需要统一的指标定义？

训练过程中，多个模块都需要查询监控指标：
- **T2-15 指标轮询采集**：恢复判定服务需要定时查询指标
- **T2-19 实时图表**：前端 WebSocket 推送实时指标数据
- **T2-20 服务健康卡片**：展示各服务健康状态
- **告警模块**：基于阈值触发告警

如果每个模块自己写 PromQL，会导致：
1. 口径不一致（有的用 rate，有的用 irate）
2. 维护困难（改一个指标要改多处）
3. 测试困难（每个模块单独测 PromQL）

T1-15 提供统一的指标定义和查询层，解决这些问题。

## 5 大核心指标

### 系统类指标

| 指标名称 | 指标 ID | 单位 | 数据来源 | 说明 |
|----------|---------|------|----------|------|
| CPU 使用率 | `cpu_usage` | % | cadvisor | 容器 CPU 使用率，0-100% |
| 内存使用率 | `memory_usage` | % | cadvisor | 容器内存使用 / 内存限制 × 100% |

### 业务类指标

| 指标名称 | 指标 ID | 单位 | 数据来源 | 说明 |
|----------|---------|------|----------|------|
| QPS | `qps` | 次/秒 | 业务 /metrics | HTTP 请求速率，2 分钟 rate |
| 错误率 | `error_rate` | % | 业务 /metrics | 5xx 错误占总请求的比例 |
| P99 延迟 | `p99_latency` | 秒 | 业务 /metrics | 第 99 百分位请求延迟 |

## PromQL 表达式

### CPU 使用率

```promql
avg by (pod) (
  rate(container_cpu_usage_seconds_total{namespace="<ns>",pod=~"<release>-.*"}[2m]) * 100
)
```

**口径说明**：
- 使用 `container_cpu_usage_seconds_total` 的 rate（2 分钟窗口）
- 乘以 100 转为百分比
- 按 pod 聚合，可进一步按服务聚合

### 内存使用率

```promql
avg by (pod) (
  container_memory_usage_bytes{namespace="<ns>",pod=~"<release>-.*"}
  /
  container_spec_memory_limit_bytes{namespace="<ns>",pod=~"<release>-.*"}
  * 100
)
```

**口径说明**：
- 使用量 / 限制量 × 100%
- 注意：如果容器没有设置 memory limit，结果会异常（分母为 0 或极大）

### QPS

```promql
avg by (service) (
  rate(http_server_requests_seconds_count{namespace="<ns>",service=~"<release>-.*"}[2m])
)
```

**口径说明**：
- 使用 `http_server_requests_seconds_count` 的 rate
- 2 分钟窗口，平滑波动
- 按 service 标签聚合

### 错误率

```promql
sum by (service) (rate(http_server_requests_seconds_count{status=~"5.."}[2m]))
/
sum by (service) (rate(http_server_requests_seconds_count[2m]))
* 100
```

**口径说明**：
- 5xx 错误数 / 总请求数 × 100%
- 先按 service sum 再相除，避免个别实例权重偏差

### P99 延迟

```promql
histogram_quantile(
  0.99,
  sum by (le, service) (
    rate(http_server_requests_seconds_bucket{namespace="<ns>",service=~"<release>-.*"}[2m])
  )
)
```

**口径说明**：
- 使用 histogram_quantile 函数计算分位数
- 先按 le 和 service 聚合 bucket，再计算分位
- 单位：秒

## 架构设计

### 分层结构

```
handler (HTTP API)
    ↓
service (业务逻辑 + 健康检查)
    ↓
PromQLBuilder (表达式构建)  ←→  metric_defs (指标定义)
    ↓
PromClient (Prometheus HTTP API)
    ↓
Prometheus (沙箱内)
```

### 核心文件

| 文件 | 职责 |
|------|------|
| `internal/metrics/metric_defs.go` | 指标定义 + PromQL 构建器 |
| `internal/metrics/prom_client.go` | Prometheus HTTP 客户端封装 |
| `internal/metrics/service.go` | 查询服务 + 健康检查 + 全量快照 |
| `internal/handler/metrics.go` | HTTP 接口（5 个端点） |
| `internal/metrics/service_test.go` | 单元测试 |

### 接口设计

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/metrics/definitions` | 列出所有核心指标定义 |
| GET | `/api/v1/metrics/query` | 区间查询（时间序列数据点） |
| GET | `/api/v1/metrics/current` | 瞬时查询（当前值） |
| GET | `/api/v1/metrics/snapshot` | 所有指标快照（一次拿 5 个） |
| GET | `/api/v1/metrics/health` | 服务健康状态检查 |

### 查询参数

公共参数：
- `metric`：指标名称（cpu_usage/memory_usage/qps/error_rate/p99_latency）
- `service`：服务名（空=所有服务聚合）
- `instance`：实例名（空=所有实例聚合）
- `aggregation`：聚合方式（avg/max/min/last）

区间查询额外参数：
- `start_time`：开始时间（毫秒时间戳）
- `end_time`：结束时间（毫秒时间戳）
- `step`：采样步长（秒）

### 健康检查

内置健康检查逻辑，供 T2-16 恢复判定和 T2-20 健康卡片使用：

| 指标 | 不健康阈值 |
|------|-----------|
| CPU 使用率 | > 90% |
| 内存使用率 | > 90% |
| 错误率 | > 5% |

**设计考虑**：
- 阈值可配置化（当前硬编码，后续可放入配置中心）
- 健康检查用 max 聚合（保守判定）
- 查询失败不影响健康状态（视为正常，避免误报）

## 与上下游的关系

### 上游依赖
- **T1-14 沙箱内 exporter 部署**：提供原始指标数据
- Prometheus 动态服务发现：自动抓取带注解的服务

### 下游使用
- **T2-15 指标轮询采集**：调用指标查询 API 做恢复判定
- **T2-19 实时图表组件**：通过 WebSocket 推送指标数据
- **T2-20 服务健康卡片**：调用健康检查接口
- **T4-11 沙箱监控后端**：管理后台查询沙箱指标

### 数据流向

```
业务服务 /metrics  ──┐
node-exporter       ├─→ Prometheus ─→ metrics.Service ─→ HTTP API ─→ 前端/其他服务
cadvisor           ─┘         ↑
                              │
                    PromQLBuilder (统一表达式)
```

## 设计要点

1. **口径统一**：所有模块使用相同的 PromQL 表达式，避免偏差
2. **分层解耦**：PromQL 构建、客户端、服务层、API 层分离，可独立测试
3. **接口化设计**：PromClient 是接口，方便 mock 测试和未来替换
4. **并发查询**：GetAllMetrics 并发查询 5 个指标，减少延迟
5. **命名空间隔离**：所有 PromQL 都带 namespace 限定，沙箱间互不干扰
6. **容错设计**：查询失败不影响健康判定（避免误报恢复）
7. **可扩展**：新增指标只需添加定义和 PromQL，不需要改架构

## 后续优化方向

1. **缓存层**：高频查询加短缓存（如 5s），减轻 Prometheus 压力
2. **指标模板**：支持自定义指标模板，运营可配置
3. **告警规则**：基于阈值的告警规则引擎
4. **多沙箱聚合**：平台侧聚合所有沙箱的指标趋势
5. **异常检测**：基于历史数据的异常检测（不仅仅是静态阈值）
6. **指标预测**：基于时间序列预测指标走势，提前告警
