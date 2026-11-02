// 指标查询服务层。
//
// 封装 Prometheus 查询为业务友好的 API：
//   - QueryMetric: 区间查询（返回时间序列数据点，用于图表）
//   - GetCurrentValue: 瞬时查询（返回当前值，用于状态判定）
//   - QueryAll: 一次查询所有 5 大核心指标（用于概览）
package metrics

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Service 指标查询服务。
type Service struct {
	prom        PromClient
	builder     *PromQLBuilder
	defaultStep int // 默认采样步长（秒）
}

// NewService 创建指标查询服务。
// prom: Prometheus 客户端
// namespace: 沙箱命名空间
// releaseName: Helm release 名称
func NewService(prom PromClient, namespace, releaseName string) *Service {
	return &Service{
		prom:        prom,
		builder:     NewPromQLBuilder(namespace, releaseName),
		defaultStep: 15,
	}
}

// QueryMetric 区间查询指标数据。
//
// 返回按时间排序的数据点列表，用于绘制趋势图。
func (s *Service) QueryMetric(ctx context.Context, req QueryRequest) (*QueryResult, error) {
	def, ok := Definitions[req.Metric]
	if !ok {
		return nil, fmt.Errorf("未知指标: %s", req.Metric)
	}

	// 校验时间范围
	if req.Start.IsZero() {
		req.Start = time.Now().Add(-15 * time.Minute) // 默认 15 分钟
	}
	if req.End.IsZero() {
		req.End = time.Now()
	}
	if req.Step <= 0 {
		req.Step = s.defaultStep
	}

	// 构建 PromQL
	expr, err := s.builder.BuildRangeQuery(req)
	if err != nil {
		return nil, fmt.Errorf("构建 PromQL 失败: %w", err)
	}

	// 执行查询
	points, err := s.prom.QueryRange(ctx, expr, req.Start, req.End, req.Step)
	if err != nil {
		return nil, fmt.Errorf("查询 Prometheus 失败: %w", err)
	}

	return &QueryResult{
		Metric:   req.Metric,
		Unit:     def.Unit,
		Service:  req.Service,
		Instance: req.Instance,
		Points:   points,
	}, nil
}

// GetCurrentValue 获取指标当前值。
//
// 用于状态判定、健康卡片等需要即时值的场景。
func (s *Service) GetCurrentValue(ctx context.Context, req InstantQueryRequest) (*InstantQueryResult, error) {
	def, ok := Definitions[req.Metric]
	if !ok {
		return nil, fmt.Errorf("未知指标: %s", req.Metric)
	}

	if req.At.IsZero() {
		req.At = time.Now()
	}

	expr, err := s.builder.BuildInstantQuery(req)
	if err != nil {
		return nil, fmt.Errorf("构建 PromQL 失败: %w", err)
	}

	value, err := s.prom.Query(ctx, expr, req.At)
	if err != nil {
		return nil, fmt.Errorf("查询 Prometheus 失败: %w", err)
	}

	return &InstantQueryResult{
		Metric:  req.Metric,
		Unit:    def.Unit,
		Service: req.Service,
		Value:   roundTo2(value),
		At:      req.At.UnixMilli(),
	}, nil
}

// AllMetricsSnapshot 所有核心指标的快照。
type AllMetricsSnapshot struct {
	Timestamp   int64              `json:"ts"` // 快照时间
	CPUUsage    MetricSnapshotItem `json:"cpu_usage"`
	MemoryUsage MetricSnapshotItem `json:"memory_usage"`
	QPS         MetricSnapshotItem `json:"qps"`
	ErrorRate   MetricSnapshotItem `json:"error_rate"`
	P99Latency  MetricSnapshotItem `json:"p99_latency"`
}

// MetricSnapshotItem 单个指标的快照值。
type MetricSnapshotItem struct {
	DisplayName string  `json:"display_name"`
	Unit        string  `json:"unit"`
	Value       float64 `json:"value"`
	Category    string  `json:"category"`
}

// GetAllMetrics 获取所有核心指标的当前快照。
//
// 用于概览页面、健康状态面板等场景。
// service: 服务名，空表示所有服务聚合
func (s *Service) GetAllMetrics(ctx context.Context, service string) (*AllMetricsSnapshot, error) {
	now := time.Now()
	result := &AllMetricsSnapshot{
		Timestamp: now.UnixMilli(),
	}

	// 并发查询 5 个指标
	type resultItem struct {
		name  MetricName
		value float64
		err   error
	}

	metrics := []MetricName{MetricCPUUsage, MetricMemoryUsage, MetricQPS, MetricErrorRate, MetricP99Latency}
	ch := make(chan resultItem, len(metrics))

	for _, m := range metrics {
		go func(name MetricName) {
			req := InstantQueryRequest{
				Metric:      name,
				Service:     service,
				At:          now,
				Aggregation: AggAvg,
			}
			val, err := s.GetCurrentValue(ctx, req)
			if err != nil {
				ch <- resultItem{name: name, err: err}
				return
			}
			ch <- resultItem{name: name, value: val.Value}
		}(m)
	}

	// 收集结果
	for range metrics {
		item := <-ch
		def := Definitions[item.name]
		snap := MetricSnapshotItem{
			DisplayName: def.DisplayName,
			Unit:        def.Unit,
			Value:       0,
			Category:    def.Category,
		}
		if item.err == nil {
			snap.Value = roundTo2(item.value)
		}

		switch item.name {
		case MetricCPUUsage:
			result.CPUUsage = snap
		case MetricMemoryUsage:
			result.MemoryUsage = snap
		case MetricQPS:
			result.QPS = snap
		case MetricErrorRate:
			result.ErrorRate = snap
		case MetricP99Latency:
			result.P99Latency = snap
		}
	}

	return result, nil
}

// ServiceHealth 服务健康状态。
type ServiceHealth struct {
	Service     string  `json:"service"`
	Healthy     bool    `json:"healthy"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	ErrorRate   float64 `json:"error_rate"`
	Reason      string  `json:"reason,omitempty"` // 不健康原因
}

// CheckServiceHealth 检查指定服务的健康状态。
//
// 判定规则（可配置，MVP 简化为固定阈值）：
//   - CPU > 90% → 不健康
//   - 内存 > 90% → 不健康
//   - 错误率 > 5% → 不健康
//
// 用于 T2-16 恢复判定和 T2-20 服务健康卡片。
func (s *Service) CheckServiceHealth(ctx context.Context, service string) (*ServiceHealth, error) {
	now := time.Now()
	health := &ServiceHealth{
		Service: service,
		Healthy: true,
	}

	// 并发查询 3 个关键指标
	type r struct {
		name  MetricName
		value float64
		err   error
	}
	ch := make(chan r, 3)

	for _, m := range []MetricName{MetricCPUUsage, MetricMemoryUsage, MetricErrorRate} {
		go func(name MetricName) {
			req := InstantQueryRequest{
				Metric:      name,
				Service:     service,
				At:          now,
				Aggregation: AggMax, // 健康检查用最大值，更保守
			}
			val, err := s.GetCurrentValue(ctx, req)
			if err != nil {
				ch <- r{name: name, err: err}
				return
			}
			ch <- r{name: name, value: val.Value}
		}(m)
	}

	for i := 0; i < 3; i++ {
		item := <-ch
		if item.err != nil {
			continue // 查询失败不影响健康判定，视为正常
		}

		switch item.name {
		case MetricCPUUsage:
			health.CPUUsage = item.value
			if item.value > 90 {
				health.Healthy = false
				health.Reason = "CPU 使用率过高"
			}
		case MetricMemoryUsage:
			health.MemoryUsage = item.value
			if item.value > 90 {
				health.Healthy = false
				health.Reason = "内存使用率过高"
			}
		case MetricErrorRate:
			health.ErrorRate = item.value
			if item.value > 5 {
				health.Healthy = false
				health.Reason = "错误率过高"
			}
		}
	}

	return health, nil
}

// ——— 工具函数 ———

func roundTo2(v float64) float64 {
	return math.Round(v*100) / 100
}
