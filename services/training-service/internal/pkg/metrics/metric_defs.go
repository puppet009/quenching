// Package metrics 核心指标定义与查询（T1-15）。
//
// 定义 5 大核心监控指标的口径和 PromQL 表达式，
// 封装 Prometheus 查询为统一的 Go API，供恢复判定、实时图表、告警等模块使用。
//
// 5 大核心指标：
//  1. CPU 使用率    - 服务 CPU 占用百分比
//  2. 内存使用率    - 服务内存占用百分比
//  3. QPS          - 每秒请求数
//  4. 错误率       - 5xx 错误占比（百分比）
//  5. P99 延迟     - 第 99 百分位请求延迟（秒）
package metrics

import (
	"fmt"
	"time"
)

// MetricName 核心指标名称。
type MetricName string

const (
	MetricCPUUsage    MetricName = "cpu_usage"    // CPU 使用率
	MetricMemoryUsage MetricName = "memory_usage" // 内存使用率
	MetricQPS         MetricName = "qps"          // 每秒请求数
	MetricErrorRate   MetricName = "error_rate"   // 错误率（5xx 占比）
	MetricP99Latency  MetricName = "p99_latency"  // P99 请求延迟（秒）
)

// MetricDef 核心指标定义。
type MetricDef struct {
	Name        MetricName `json:"name"`         // 指标名称
	DisplayName string     `json:"display_name"` // 展示名称
	Unit        string     `json:"unit"`         // 单位（%、次/秒、秒）
	Category    string     `json:"category"`     // 分类：system / business
	Description string     `json:"description"`  // 指标说明
}

// Definitions 所有核心指标定义。
var Definitions = map[MetricName]MetricDef{
	MetricCPUUsage: {
		Name:        MetricCPUUsage,
		DisplayName: "CPU 使用率",
		Unit:        "%",
		Category:    "system",
		Description: "服务进程的 CPU 使用率，0-100%",
	},
	MetricMemoryUsage: {
		Name:        MetricMemoryUsage,
		DisplayName: "内存使用率",
		Unit:        "%",
		Category:    "system",
		Description: "服务进程的内存使用率，0-100%",
	},
	MetricQPS: {
		Name:        MetricQPS,
		DisplayName: "QPS",
		Unit:        "次/秒",
		Category:    "business",
		Description: "每秒处理的 HTTP 请求数",
	},
	MetricErrorRate: {
		Name:        MetricErrorRate,
		DisplayName: "错误率",
		Unit:        "%",
		Category:    "business",
		Description: "5xx 错误请求占总请求的比例",
	},
	MetricP99Latency: {
		Name:        MetricP99Latency,
		DisplayName: "P99 延迟",
		Unit:        "秒",
		Category:    "business",
		Description: "第 99 百分位请求延迟，即 99% 的请求快于该值",
	},
}

// ListMetrics 列出所有核心指标定义。
func ListMetrics() []MetricDef {
	result := make([]MetricDef, 0, len(Definitions))
	for _, def := range Definitions {
		result = append(result, def)
	}
	return result
}

// GetMetricDef 获取指定指标的定义。
func GetMetricDef(name MetricName) (MetricDef, bool) {
	def, ok := Definitions[name]
	return def, ok
}

// Aggregation 聚合方式。
type Aggregation string

const (
	AggAvg  Aggregation = "avg"  // 平均值
	AggMax  Aggregation = "max"  // 最大值
	AggMin  Aggregation = "min"  // 最小值
	AggLast Aggregation = "last" // 最新值
	AggP99  Aggregation = "p99"  // 第 99 百分位（用于分布型指标）
	AggRate Aggregation = "rate" // 速率（counter 类型专用）
)

// QueryRequest 指标查询请求。
type QueryRequest struct {
	Metric      MetricName  `json:"metric"`      // 指标名称
	Service     string      `json:"service"`     // 服务名（空=全部服务聚合）
	Instance    string      `json:"instance"`    // 实例名（空=全部实例聚合）
	Start       time.Time   `json:"start"`       // 开始时间
	End         time.Time   `json:"end"`         // 结束时间
	Step        int         `json:"step"`        // 采样步长（秒），默认 15
	Aggregation Aggregation `json:"aggregation"` // 聚合方式，默认 avg
}

// QueryResult 指标查询结果。
type QueryResult struct {
	Metric   MetricName  `json:"metric"`   // 指标名称
	Unit     string      `json:"unit"`     // 单位
	Service  string      `json:"service"`  // 服务名
	Instance string      `json:"instance"` // 实例名
	Points   []DataPoint `json:"points"`   // 数据点
}

// DataPoint 单个数据点。
type DataPoint struct {
	Timestamp int64   `json:"ts"`    // 时间戳（毫秒）
	Value     float64 `json:"value"` // 指标值
}

// InstantQueryRequest 瞬时值查询请求。
type InstantQueryRequest struct {
	Metric      MetricName  `json:"metric"`      // 指标名称
	Service     string      `json:"service"`     // 服务名
	Instance    string      `json:"instance"`    // 实例名
	At          time.Time   `json:"at"`          // 查询时间点
	Aggregation Aggregation `json:"aggregation"` // 聚合方式
}

// InstantQueryResult 瞬时值查询结果。
type InstantQueryResult struct {
	Metric  MetricName `json:"metric"`  // 指标名称
	Unit    string     `json:"unit"`    // 单位
	Service string     `json:"service"` // 服务名
	Value   float64    `json:"value"`   // 当前值
	At      int64      `json:"at"`      // 时间戳（毫秒）
}

// ——— PromQL 表达式构建 ———

// PromQLBuilder PromQL 查询构建器。
// 根据指标、服务、聚合方式等参数生成对应的 PromQL 表达式。
//
// 说明：
//   - 系统指标（CPU/内存）从 cadvisor 或 node-exporter 获取
//   - 业务指标（QPS/错误率/P99）从业务服务的 /metrics 端点获取
type PromQLBuilder struct {
	// Namespace 沙箱命名空间，用于限定查询范围
	Namespace string
	// ReleaseName Helm release 名称，用于服务名匹配
	ReleaseName string
}

// NewPromQLBuilder 创建 PromQL 构建器。
func NewPromQLBuilder(namespace, releaseName string) *PromQLBuilder {
	return &PromQLBuilder{
		Namespace:   namespace,
		ReleaseName: releaseName,
	}
}

// BuildRangeQuery 构建区间查询的 PromQL 表达式。
func (b *PromQLBuilder) BuildRangeQuery(req QueryRequest) (string, error) {
	expr, err := b.buildBaseExpr(req.Metric, req.Service, req.Instance, req.Aggregation)
	if err != nil {
		return "", err
	}
	return expr, nil
}

// BuildInstantQuery 构建瞬时查询的 PromQL 表达式。
func (b *PromQLBuilder) BuildInstantQuery(req InstantQueryRequest) (string, error) {
	expr, err := b.buildBaseExpr(req.Metric, req.Service, req.Instance, req.Aggregation)
	if err != nil {
		return "", err
	}
	return expr, nil
}

// buildBaseExpr 构建基础 PromQL 表达式。
func (b *PromQLBuilder) buildBaseExpr(metric MetricName, service string, instance string, agg Aggregation) (string, error) {
	switch metric {
	case MetricCPUUsage:
		return b.buildCPUExpr(service, agg)
	case MetricMemoryUsage:
		return b.buildMemoryExpr(service, agg)
	case MetricQPS:
		return b.buildQPSExpr(service, agg)
	case MetricErrorRate:
		return b.buildErrorRateExpr(service, agg)
	case MetricP99Latency:
		return b.buildP99Expr(service, agg)
	default:
		return "", fmt.Errorf("未知指标: %s", metric)
	}
}

// buildCPUExpr CPU 使用率 PromQL。
// 来源：cadvisor 的 container_cpu_usage_seconds_total
func (b *PromQLBuilder) buildCPUExpr(service string, agg Aggregation) (string, error) {
	// container_cpu_usage_seconds_total 是 counter，取 rate 再乘 100
	base := fmt.Sprintf(
		`rate(container_cpu_usage_seconds_total{namespace="%s",container_label_io_kubernetes_pod_name=~".*%s.*"}[2m]) * 100`,
		b.Namespace, b.releasePrefix(service))

	return applyAggregation(base, agg, "pod"), nil
}

// buildMemoryExpr 内存使用率 PromQL。
func (b *PromQLBuilder) buildMemoryExpr(service string, agg Aggregation) (string, error) {
	// 内存使用 / 内存限制 * 100
	base := fmt.Sprintf(
		`container_memory_usage_bytes{namespace="%s",container_label_io_kubernetes_pod_name=~".*%s.*"} / container_spec_memory_limit_bytes{namespace="%s",container_label_io_kubernetes_pod_name=~".*%s.*"} * 100`,
		b.Namespace, b.releasePrefix(service),
		b.Namespace, b.releasePrefix(service))

	return applyAggregation(base, agg, "pod"), nil
}

// buildQPSExpr QPS PromQL。
// 来源：Spring Boot Actuator 的 http_server_requests_seconds_count
func (b *PromQLBuilder) buildQPSExpr(service string, agg Aggregation) (string, error) {
	base := fmt.Sprintf(
		`rate(http_server_requests_seconds_count{namespace="%s",service=~".*%s.*"}[2m])`,
		b.Namespace, b.releasePrefix(service))

	return applyAggregation(base, agg, "service"), nil
}

// buildErrorRateExpr 错误率 PromQL。
// 5xx 错误数 / 总请求数 * 100
func (b *PromQLBuilder) buildErrorRateExpr(service string, agg Aggregation) (string, error) {
	base := fmt.Sprintf(
		`sum by (service) (rate(http_server_requests_seconds_count{namespace="%s",service=~".*%s.*",status=~"5.."}[2m])) / sum by (service) (rate(http_server_requests_seconds_count{namespace="%s",service=~".*%s.*"}[2m])) * 100`,
		b.Namespace, b.releasePrefix(service),
		b.Namespace, b.releasePrefix(service))

	// 错误率是比值，外层不再做聚合（已经是按 service 聚合后的结果）
	if agg == AggAvg || agg == "" {
		return base, nil
	}
	return applyAggregation(base, agg, "service"), nil
}

// buildP99Expr P99 延迟 PromQL。
// 来源：http_server_requests_seconds histogram
func (b *PromQLBuilder) buildP99Expr(service string, agg Aggregation) (string, error) {
	base := fmt.Sprintf(
		`histogram_quantile(0.99, sum by (le, service) (rate(http_server_requests_seconds_bucket{namespace="%s",service=~".*%s.*"}[2m])))`,
		b.Namespace, b.releasePrefix(service))

	// P99 是按 service 计算后的分位值，外层用 max 聚合多个实例
	if agg == AggMax || agg == "" {
		return base, nil
	}
	return applyAggregation(base, agg, "service"), nil
}

// releasePrefix 生成 release 前缀的服务名正则。
// 如果 service 为空，匹配所有服务；否则精确匹配。
func (b *PromQLBuilder) releasePrefix(service string) string {
	if service == "" {
		return b.ReleaseName
	}
	return fmt.Sprintf("%s-%s", b.ReleaseName, service)
}

// applyAggregation 应用聚合函数。
func applyAggregation(expr string, agg Aggregation, byLabel string) string {
	switch agg {
	case AggMax:
		return fmt.Sprintf("max(%s) by (%s)", expr, byLabel)
	case AggMin:
		return fmt.Sprintf("min(%s) by (%s)", expr, byLabel)
	case AggAvg, "":
		return fmt.Sprintf("avg(%s) by (%s)", expr, byLabel)
	case AggLast:
		// last_value 没有 by 子句语义，用 max 近似（监控场景下最后值和最大值接近）
		return fmt.Sprintf("max(%s) by (%s)", expr, byLabel)
	default:
		return fmt.Sprintf("avg(%s) by (%s)", expr, byLabel)
	}
}
