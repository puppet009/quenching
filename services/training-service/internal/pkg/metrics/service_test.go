// 核心指标定义与查询单元测试（T1-15）。
package metrics

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ========== 指标定义测试 ==========

func TestListMetrics(t *testing.T) {
	defs := ListMetrics()

	// 应有 5 个核心指标
	if len(defs) != 5 {
		t.Errorf("应有 5 个核心指标, got %d", len(defs))
	}
}

func TestGetMetricDef(t *testing.T) {
	// CPU 指标
	def, ok := GetMetricDef(MetricCPUUsage)
	if !ok {
		t.Fatal("CPU 指标应存在")
	}
	if def.DisplayName != "CPU 使用率" {
		t.Errorf("CPU 显示名应为'CPU 使用率', got %s", def.DisplayName)
	}
	if def.Unit != "%" {
		t.Errorf("CPU 单位应为'%%', got %s", def.Unit)
	}
	if def.Category != "system" {
		t.Errorf("CPU 分类应为'system', got %s", def.Category)
	}

	// P99 指标
	def, ok = GetMetricDef(MetricP99Latency)
	if !ok {
		t.Fatal("P99 指标应存在")
	}
	if def.Category != "business" {
		t.Errorf("P99 分类应为'business', got %s", def.Category)
	}
	if def.Unit != "秒" {
		t.Errorf("P99 单位应为'秒', got %s", def.Unit)
	}
}

func TestGetMetricDef_Unknown(t *testing.T) {
	_, ok := GetMetricDef("unknown_metric")
	if ok {
		t.Error("未知指标不应返回 true")
	}
}

// ========== PromQL 构建器测试 ==========

func TestPromQLBuilder_CPUUsage(t *testing.T) {
	b := NewPromQLBuilder("sandbox-123", "ecom")

	req := QueryRequest{
		Metric:      MetricCPUUsage,
		Service:     "user-service",
		Aggregation: AggAvg,
	}

	expr, err := b.BuildRangeQuery(req)
	if err != nil {
		t.Fatal(err)
	}

	// 验证包含关键元素
	if !contains(expr, "container_cpu_usage_seconds_total") {
		t.Errorf("CPU PromQL 应包含 container_cpu_usage_seconds_total, got %s", expr)
	}
	if !contains(expr, "sandbox-123") {
		t.Errorf("PromQL 应包含 namespace, got %s", expr)
	}
	if !contains(expr, "ecom-user-service") {
		t.Errorf("PromQL 应包含服务名前缀, got %s", expr)
	}
	if !contains(expr, "rate(") {
		t.Errorf("CPU PromQL 应使用 rate 函数, got %s", expr)
	}
}

func TestPromQLBuilder_MemoryUsage(t *testing.T) {
	b := NewPromQLBuilder("sandbox-123", "ecom")

	req := QueryRequest{
		Metric:      MetricMemoryUsage,
		Service:     "order-service",
		Aggregation: AggMax,
	}

	expr, err := b.BuildRangeQuery(req)
	if err != nil {
		t.Fatal(err)
	}

	if !contains(expr, "container_memory_usage_bytes") {
		t.Errorf("内存 PromQL 应包含 container_memory_usage_bytes, got %s", expr)
	}
	if !contains(expr, "container_spec_memory_limit_bytes") {
		t.Errorf("内存 PromQL 应包含 limit_bytes, got %s", expr)
	}
}

func TestPromQLBuilder_QPS(t *testing.T) {
	b := NewPromQLBuilder("sandbox-123", "ecom")

	req := QueryRequest{
		Metric:      MetricQPS,
		Service:     "product-service",
		Aggregation: AggAvg,
	}

	expr, err := b.BuildRangeQuery(req)
	if err != nil {
		t.Fatal(err)
	}

	if !contains(expr, "http_server_requests_seconds_count") {
		t.Errorf("QPS PromQL 应包含 http_server_requests_seconds_count, got %s", expr)
	}
}

func TestPromQLBuilder_ErrorRate(t *testing.T) {
	b := NewPromQLBuilder("sandbox-123", "ecom")

	req := QueryRequest{
		Metric:      MetricErrorRate,
		Service:     "order-service",
		Aggregation: AggAvg,
	}

	expr, err := b.BuildRangeQuery(req)
	if err != nil {
		t.Fatal(err)
	}

	if !contains(expr, "status=~\"5..\"") {
		t.Errorf("错误率 PromQL 应包含 5xx 状态过滤, got %s", expr)
	}
}

func TestPromQLBuilder_P99Latency(t *testing.T) {
	b := NewPromQLBuilder("sandbox-123", "ecom")

	req := QueryRequest{
		Metric:      MetricP99Latency,
		Service:     "user-service",
		Aggregation: AggAvg,
	}

	expr, err := b.BuildRangeQuery(req)
	if err != nil {
		t.Fatal(err)
	}

	if !contains(expr, "histogram_quantile(0.99") {
		t.Errorf("P99 PromQL 应包含 histogram_quantile(0.99, got %s", expr)
	}
	if !contains(expr, "http_server_requests_seconds_bucket") {
		t.Errorf("P99 PromQL 应包含 _bucket, got %s", expr)
	}
}

func TestPromQLBuilder_AllServices(t *testing.T) {
	b := NewPromQLBuilder("sandbox-123", "ecom")

	// service 为空时匹配所有服务
	req := QueryRequest{
		Metric:      MetricCPUUsage,
		Service:     "",
		Aggregation: AggAvg,
	}

	expr, err := b.BuildRangeQuery(req)
	if err != nil {
		t.Fatal(err)
	}

	if !contains(expr, "ecom.*") {
		t.Errorf("空 service 时应匹配所有服务, got %s", expr)
	}
}

func TestPromQLBuilder_UnknownMetric(t *testing.T) {
	b := NewPromQLBuilder("sandbox-123", "ecom")

	req := QueryRequest{
		Metric: "unknown",
	}

	_, err := b.BuildRangeQuery(req)
	if err == nil {
		t.Error("未知指标应返回错误")
	}
}

// ========== Mock Prometheus 客户端 ==========

type fakePromClient struct {
	mu         sync.Mutex
	rangeData  []DataPoint
	instantVal float64
	// instantVals 按 PromQL 特征子串覆盖瞬时值（如 error_rate 查询）。
	instantVals map[string]float64
	lastQuery   string
}

func (f *fakePromClient) QueryRange(_ context.Context, query string, _, _ time.Time, _ int) ([]DataPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = query
	return f.rangeData, nil
}

func (f *fakePromClient) Query(_ context.Context, query string, _ time.Time) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = query
	for marker, v := range f.instantVals {
		if strings.Contains(query, marker) {
			return v, nil
		}
	}
	return f.instantVal, nil
}

type blockingPromClient struct {
	fakePromClient
	entered chan struct{}
	release chan struct{}
}

func (f *blockingPromClient) Query(ctx context.Context, query string, at time.Time) (float64, error) {
	f.entered <- struct{}{}
	select {
	case <-f.release:
		return f.fakePromClient.Query(ctx, query, at)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestService_MetricQueriesRemainConcurrent(t *testing.T) {
	for _, test := range []struct {
		name    string
		queries int
		run     func(context.Context, *Service) error
	}{
		{"all metrics", 5, func(ctx context.Context, svc *Service) error {
			_, err := svc.GetAllMetrics(ctx, "user-service")
			return err
		}},
		{"service health", 3, func(ctx context.Context, svc *Service) error {
			_, err := svc.CheckServiceHealth(ctx, "user-service")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := &blockingPromClient{
				entered: make(chan struct{}, test.queries), release: make(chan struct{}),
			}
			unblock := sync.OnceFunc(func() { close(client.release) })
			defer unblock()
			done := make(chan error, 1)
			go func() { done <- test.run(ctx, NewService(client, "sandbox-123", "ecom")) }()
			for range test.queries {
				select {
				case <-client.entered:
				case <-ctx.Done():
					t.Fatal("metric queries must all start before any query completes")
				}
			}
			// All queries entered concurrently; let each return through the shared fake.
			unblock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("concurrent metric collection did not complete")
			}
		})
	}
}

// ========== 服务层测试 ==========

func TestService_QueryMetric(t *testing.T) {
	fake := &fakePromClient{
		rangeData: []DataPoint{
			{Timestamp: 1000000000000, Value: 45.5},
			{Timestamp: 1000000015000, Value: 48.2},
			{Timestamp: 1000000030000, Value: 50.1},
		},
	}

	svc := NewService(fake, "sandbox-123", "ecom")

	result, err := svc.QueryMetric(context.Background(), QueryRequest{
		Metric:      MetricCPUUsage,
		Service:     "user-service",
		Aggregation: AggAvg,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Metric != MetricCPUUsage {
		t.Errorf("指标名应为 cpu_usage, got %s", result.Metric)
	}
	if result.Unit != "%" {
		t.Errorf("单位应为 %%, got %s", result.Unit)
	}
	if len(result.Points) != 3 {
		t.Errorf("应有 3 个数据点, got %d", len(result.Points))
	}
	if result.Points[0].Value != 45.5 {
		t.Errorf("第一个数据点值应为 45.5, got %.2f", result.Points[0].Value)
	}
}

func TestService_GetCurrentValue(t *testing.T) {
	fake := &fakePromClient{instantVal: 42.5}
	svc := NewService(fake, "sandbox-123", "ecom")

	result, err := svc.GetCurrentValue(context.Background(), InstantQueryRequest{
		Metric:  MetricCPUUsage,
		Service: "order-service",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Value != 42.5 {
		t.Errorf("当前值应为 42.5, got %.2f", result.Value)
	}
	if result.Unit != "%" {
		t.Errorf("单位应为 %%, got %s", result.Unit)
	}
	if result.At == 0 {
		t.Error("时间戳不应为 0")
	}
}

func TestService_GetAllMetrics(t *testing.T) {
	fake := &fakePromClient{instantVal: 30.0}
	svc := NewService(fake, "sandbox-123", "ecom")

	snapshot, err := svc.GetAllMetrics(context.Background(), "user-service")
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.CPUUsage.Value != 30.0 {
		t.Errorf("CPU 应为 30.0, got %.2f", snapshot.CPUUsage.Value)
	}
	if snapshot.CPUUsage.DisplayName != "CPU 使用率" {
		t.Errorf("CPU 显示名应为'CPU 使用率', got %s", snapshot.CPUUsage.DisplayName)
	}
	if snapshot.QPS.Unit != "次/秒" {
		t.Errorf("QPS 单位应为'次/秒', got %s", snapshot.QPS.Unit)
	}
	if snapshot.P99Latency.Unit != "秒" {
		t.Errorf("P99 单位应为'秒', got %s", snapshot.P99Latency.Unit)
	}
	if snapshot.Timestamp == 0 {
		t.Error("时间戳不应为 0")
	}
}

func TestService_CheckServiceHealth_Healthy(t *testing.T) {
	// CPU/内存 30% 在正常范围内；错误率单独给 1%（共用 fake 时避免被阈值 5% 误伤）。
	fake := &fakePromClient{
		instantVal:  30.0,
		instantVals: map[string]float64{`status=~"5.."`: 1.0},
	}
	svc := NewService(fake, "sandbox-123", "ecom")

	health, err := svc.CheckServiceHealth(context.Background(), "user-service")
	if err != nil {
		t.Fatal(err)
	}

	if !health.Healthy {
		t.Error("指标都正常时应判定为健康")
	}
	if health.CPUUsage != 30.0 {
		t.Errorf("CPU 应为 30.0, got %.2f", health.CPUUsage)
	}
}

func TestService_CheckServiceHealth_CPUHigh(t *testing.T) {
	// CPU 95%，超过 90% 阈值
	fake := &fakePromClient{instantVal: 95.0}
	svc := NewService(fake, "sandbox-123", "ecom")

	health, err := svc.CheckServiceHealth(context.Background(), "user-service")
	if err != nil {
		t.Fatal(err)
	}

	if health.Healthy {
		t.Error("CPU 过高时应判定为不健康")
	}
	if health.Reason == "" {
		t.Error("不健康时应有原因")
	}
}

func TestService_CheckServiceHealth_ErrorRateHigh(t *testing.T) {
	// 错误率 10%，超过 5% 阈值
	fake := &fakePromClient{instantVal: 10.0}
	svc := NewService(fake, "sandbox-123", "ecom")

	health, err := svc.CheckServiceHealth(context.Background(), "user-service")
	if err != nil {
		t.Fatal(err)
	}

	if health.Healthy {
		t.Error("错误率过高时应判定为不健康")
	}
}

func TestService_QueryMetric_UnknownMetric(t *testing.T) {
	fake := &fakePromClient{}
	svc := NewService(fake, "sandbox-123", "ecom")

	_, err := svc.QueryMetric(context.Background(), QueryRequest{
		Metric: "unknown",
	})
	if err == nil {
		t.Error("未知指标应返回错误")
	}
}

// ========== 工具函数测试 ==========

func TestRoundTo2(t *testing.T) {
	cases := []struct {
		input    float64
		expected float64
	}{
		{3.14159, 3.14},
		{3.145, 3.15},
		{0, 0},
		{-1.234, -1.23},
		{100.0, 100.0},
	}

	for _, c := range cases {
		result := roundTo2(c.input)
		if result != c.expected {
			t.Errorf("roundTo2(%.5f) = %.2f, 期望 %.2f", c.input, result, c.expected)
		}
	}
}

// ========== 辅助函数 ==========

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// 避免 fmt 未使用警告
var _ = fmt.Sprintf
