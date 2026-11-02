# 沙箱监控后端（T4-11）

管理后台可查活跃沙箱状态、实时资源使用，支持手动释放和异常告警。

## 交付物

```
internal/
  model/monitor.go               # 资源指标 + 异常模型 + 查询结构
  service/monitor_store.go       # MonitorStore 接口定义
  service/inmemory_monitor.go    # 内存版实现（开发/测试用）
  service/monitor.go             # MonitorService（监控+异常检测+告警）
  handler/monitor.go             # 管理后台 8 个 HTTP 接口
tests/monitor_test.go            # 20+ 测试用例
docs/sandbox_monitor.md          # 本文档
```

## 8 个管理后台接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/v1/sandboxes` | 沙箱列表（分页+4种筛选） |
| GET | `/api/admin/v1/sandboxes/stats` | 沙箱统计概览 |
| GET | `/api/admin/v1/sandboxes/:session_id` | 沙箱详情（状态/时长/指标） |
| DELETE | `/api/admin/v1/sandboxes/:session_id` | 手动释放沙箱 |
| GET | `/api/admin/v1/sandboxes/:session_id/metrics` | 最新资源指标 |
| GET | `/api/admin/v1/sandboxes/:session_id/metrics/history` | 指标历史（趋势） |
| GET | `/api/admin/v1/anomalies` | 异常告警列表 |
| PATCH | `/api/admin/v1/anomalies/:id/resolve` | 标记异常已解决 |

## 列表筛选维度

| 参数 | 说明 |
|------|------|
| user_id | 按用户筛选 |
| project_id | 按项目筛选 |
| status | 按状态筛选（creating/deploying/ready/running/releasing/released/failed） |
| namespace | 命名空间模糊搜索 |
| is_active | 是否活跃（true=未释放, false=已释放/失败） |
| page / page_size | 分页 |

## 资源监控指标

```go
type ResourceMetrics struct {
    CPUCoresM     float64 // CPU 使用（毫核）
    CPUPercent    float64 // CPU 使用率（%）
    MemoryBytes   int64   // 内存使用（字节）
    MemoryPercent float64 // 内存使用率（%）
    PodCount      int     // Pod 总数
    PodRunning    int     // 运行中 Pod 数
    PodAbnormal   int     // 异常 Pod 数（Failed/Pending）
}
```

采集方式：
- 生产：Prometheus / metrics-server 定时采集
- 开发：内存模拟数据

## 异常检测

### 5 种异常类型

| 类型 | 说明 | 警告阈值 | 严重阈值 |
|------|------|---------|---------|
| high_cpu | CPU 过高 | 70% | 90% |
| high_memory | 内存过高 | 75% | 90% |
| failed_pod | Pod 异常 | 1 个 | 2 个 |
| long_running | 运行时间过长 | 90 分钟 | 110 分钟 |
| stuck_status | 状态卡住（creating/deploying） | - | 10 分钟 |

### 两级告警

- **warning**：接近阈值，提示关注
- **error**：超过阈值，需要干预

### 告警回调

```go
svc.SetAlertCallback(func(ctx context.Context, anomaly *model.SandboxAnomaly) {
    // 接入飞书/钉钉/邮件/监控系统
    alerting.Notify(ctx, anomaly)
})
```

## 手动释放

1. 前端调用 DELETE 接口
2. 后端将沙箱状态标记为 `releasing`
3. 调谐器（Reconciler）异步执行实际释放（删除 namespace 等）
4. 释放完成后状态更新为 `released`

好处：
- 接口快速返回，不阻塞
- 释放逻辑复用调谐器，保证幂等
- 状态流转清晰可追踪

## 设计要点

### 1. 存储接口抽象

MonitorStore 接口定义所有数据操作：
- 内存实现（开发/测试）：线程安全，零依赖
- PG 实现（生产）：替换实现即可，接口不变

### 2. 异常检测与指标上报解耦

ReportMetrics 同时做：
- 保存指标 → 历史记录
- 异常检测 → 触发告警

两种定时检测：
- DetectStuckStatuses：状态卡住检测（每分钟）
- DetectLongRunning：长时间运行检测（每 5 分钟）

### 3. 沙箱详情增强

详情包含：
- 基础信息（用户/项目/状态/命名空间）
- 运行时长（秒）
- 剩余存活时间（秒）
- 最新资源指标（CPU/内存/Pod）

前端可直接渲染完整的沙箱监控卡片。

### 4. 统计概览

`/stats` 接口返回：
- 总沙箱数
- 活跃沙箱数
- 各状态分布
- 未解决异常数

用于管理后台 Dashboard 顶部统计卡片。

## 生产环境接入

### 指标采集

实现 MetricsCollector 接口：
- 从 Prometheus 查询（推荐，精度高）
- 从 metrics-server 拉取（轻量，实时性好）
- 直接调用 K8s API（简单，适合小规模）

### 告警通知

通过 SetAlertCallback 接入：
- 飞书机器人
- 钉钉机器人
- 企业微信
- 邮件
- 内部监控系统（Prometheus Alertmanager）

### 持久化

替换 InMemoryMonitorStore 为 PostgreSQL 实现：
- sandboxes 表（已有）
- sandbox_metrics 表（指标历史，建议时序库或带分区）
- sandbox_anomalies 表（异常记录）

## 安全考虑

1. **管理接口鉴权**：所有 `/api/admin/v1/*` 接口需要管理员权限
2. **手动释放审计**：记录释放操作人和原因（可扩展）
3. **释放保护**：已释放或释放中的沙箱不可重复操作
4. **指标数据量控制**：历史指标最多保留 N 条或 N 天，避免无限增长
