// 健康卡片单元测试（T1-20）。
package service

import (
	"context"
	"testing"
	"time"
)

// ========== Mock 数据 ==========

type fakeHealthMetrics struct {
	healthMap map[string]ServiceHealth
	snapshot  AllMetricsSnapshot
}

func (f *fakeHealthMetrics) CheckServiceHealth(_ context.Context, service string) (ServiceHealth, error) {
	if h, ok := f.healthMap[service]; ok {
		return h, nil
	}
	return ServiceHealth{Service: service, Healthy: true}, nil
}

func (f *fakeHealthMetrics) GetAllMetrics(_ context.Context, service string) (AllMetricsSnapshot, error) {
	return f.snapshot, nil
}

type fakeHealthAlerts struct {
	alertMap map[string]AlertCount
}

func (f *fakeHealthAlerts) ListAlerts(_ string, _ string) []AlertInfo {
	return nil
}

func (f *fakeHealthAlerts) AlertCountByService(_ string) map[string]AlertCount {
	if f.alertMap != nil {
		return f.alertMap
	}
	return make(map[string]AlertCount)
}

type fakeHealthPublisher struct {
	pushed    bool
	channel   string
	eventType string
}

func (f *fakeHealthPublisher) PublishToChannel(_ context.Context, channel, eventType string, _ interface{}) error {
	f.pushed = true
	f.channel = channel
	f.eventType = eventType
	return nil
}

// ========== 健康状态判定测试 ==========

func TestDetermineStatus_Healthy(t *testing.T) {
	svc := NewHealthCardService(nil, nil, nil)

	health := ServiceHealth{Healthy: true}
	alerts := AlertCount{}

	status := svc.determineStatus(health, alerts)
	if status != StatusHealthy {
		t.Errorf("应为 healthy, got %s", status)
	}
}

func TestDetermineStatus_WarningAlerts(t *testing.T) {
	svc := NewHealthCardService(nil, nil, nil)

	health := ServiceHealth{Healthy: true}
	alerts := AlertCount{Warning: 2}

	status := svc.determineStatus(health, alerts)
	if status != StatusWarning {
		t.Errorf("有 warning 告警应为 warning, got %s", status)
	}
}

func TestDetermineStatus_CriticalAlerts(t *testing.T) {
	svc := NewHealthCardService(nil, nil, nil)

	health := ServiceHealth{Healthy: true}
	alerts := AlertCount{Critical: 1}

	status := svc.determineStatus(health, alerts)
	if status != StatusCritical {
		t.Errorf("有 critical 告警应为 critical, got %s", status)
	}
}

func TestDetermineStatus_UnhealthyMetrics(t *testing.T) {
	svc := NewHealthCardService(nil, nil, nil)

	health := ServiceHealth{Healthy: false, Reason: "CPU 过高"}
	alerts := AlertCount{}

	status := svc.determineStatus(health, alerts)
	if status != StatusCritical {
		t.Errorf("指标不健康应为 critical, got %s", status)
	}
}

func TestDetermineStatus_Both(t *testing.T) {
	svc := NewHealthCardService(nil, nil, nil)

	// 既有 warning 告警，指标也不健康 → 应是 critical
	health := ServiceHealth{Healthy: false}
	alerts := AlertCount{Warning: 3}

	status := svc.determineStatus(health, alerts)
	if status != StatusCritical {
		t.Errorf("指标不健康+warning告警应为 critical, got %s", status)
	}
}

// ========== 服务卡片构建测试 ==========

func TestBuildServiceCard_Healthy(t *testing.T) {
	metrics := &fakeHealthMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service": {Service: "user-service", Healthy: true, CPUUsage: 30.0, MemoryUsage: 45.0, ErrorRate: 0.5},
		},
		snapshot: AllMetricsSnapshot{
			CPUUsage:    MetricValue{Value: 30.0, Unit: "%"},
			MemoryUsage: MetricValue{Value: 45.0, Unit: "%"},
			QPS:         MetricValue{Value: 120.5, Unit: "次/秒"},
			ErrorRate:   MetricValue{Value: 0.5, Unit: "%"},
			P99Latency:  MetricValue{Value: 0.05, Unit: "秒"},
		},
	}

	alerts := &fakeHealthAlerts{}
	svc := NewHealthCardService(metrics, alerts, nil)

	card, err := svc.buildServiceCard(context.Background(), "sandbox-123", "user-service", AlertCount{})
	if err != nil {
		t.Fatal(err)
	}

	if card.Status != StatusHealthy {
		t.Errorf("状态应为 healthy, got %s", card.Status)
	}
	if card.CPUUsage != 30.0 {
		t.Errorf("CPU 应为 30.0, got %.2f", card.CPUUsage)
	}
	if card.QPS != 120.5 {
		t.Errorf("QPS 应为 120.5, got %.2f", card.QPS)
	}
	if card.P99Latency != 0.05 {
		t.Errorf("P99 应为 0.05, got %.4f", card.P99Latency)
	}
	if card.AlertCount != 0 {
		t.Errorf("告警数应为 0, got %d", card.AlertCount)
	}
}

func TestBuildServiceCard_WithAlerts(t *testing.T) {
	metrics := &fakeHealthMetrics{
		healthMap: map[string]ServiceHealth{
			"order-service": {Service: "order-service", Healthy: true, CPUUsage: 50.0, MemoryUsage: 60.0, ErrorRate: 1.0},
		},
		snapshot: AllMetricsSnapshot{
			QPS:        MetricValue{Value: 80.0, Unit: "次/秒"},
			P99Latency: MetricValue{Value: 0.1, Unit: "秒"},
		},
	}

	alerts := &fakeHealthAlerts{}
	svc := NewHealthCardService(metrics, alerts, nil)

	card, err := svc.buildServiceCard(context.Background(), "sandbox-123", "order-service", AlertCount{
		Total:    3,
		Critical: 1,
		Warning:  2,
	})
	if err != nil {
		t.Fatal(err)
	}

	if card.Status != StatusCritical {
		t.Errorf("有 critical 告警应为 critical, got %s", card.Status)
	}
	if card.CriticalCount != 1 {
		t.Errorf("critical 数应为 1, got %d", card.CriticalCount)
	}
	if card.AlertCount != 3 {
		t.Errorf("告警总数应为 3, got %d", card.AlertCount)
	}
	if card.Reason == "" {
		t.Error("critical 状态应有原因")
	}
}

// ========== 概览构建测试 ==========

func TestBuildOverview(t *testing.T) {
	metrics := &fakeHealthMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":    {Service: "user-service", Healthy: true, CPUUsage: 30.0, MemoryUsage: 40.0},
			"product-service": {Service: "product-service", Healthy: true, CPUUsage: 50.0, MemoryUsage: 55.0},
			"order-service":   {Service: "order-service", Healthy: false, CPUUsage: 95.0, MemoryUsage: 70.0, Reason: "CPU 过高"},
		},
		snapshot: AllMetricsSnapshot{
			QPS:        MetricValue{Value: 100.0, Unit: "次/秒"},
			P99Latency: MetricValue{Value: 0.1, Unit: "秒"},
		},
	}

	alerts := &fakeHealthAlerts{
		alertMap: map[string]AlertCount{
			"order-service": {Total: 2, Critical: 1, Warning: 1},
		},
	}

	svc := NewHealthCardService(metrics, alerts, nil)
	svc.SetServices([]string{"user-service", "product-service", "order-service"})

	overview, err := svc.buildOverview(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	if overview.TotalServices != 3 {
		t.Errorf("应有 3 个服务, got %d", overview.TotalServices)
	}
	if overview.HealthyCount != 2 {
		t.Errorf("应有 2 个健康服务, got %d", overview.HealthyCount)
	}
	if overview.CriticalCount != 1 {
		t.Errorf("应有 1 个严重服务, got %d", overview.CriticalCount)
	}
	if overview.TotalAlerts != 2 {
		t.Errorf("总告警数应为 2, got %d", overview.TotalAlerts)
	}
	if overview.SandboxID != "sandbox-123" {
		t.Errorf("沙箱ID不匹配")
	}
}

// ========== 排序测试 ==========

func TestSortServicesBySeverity(t *testing.T) {
	services := []ServiceCard{
		{Service: "user", Status: StatusHealthy},
		{Service: "order", Status: StatusCritical},
		{Service: "product", Status: StatusWarning},
	}

	sortServicesBySeverity(services)

	if services[0].Status != StatusCritical {
		t.Errorf("第一个应为 critical, got %s", services[0].Status)
	}
	if services[1].Status != StatusWarning {
		t.Errorf("第二个应为 warning, got %s", services[1].Status)
	}
	if services[2].Status != StatusHealthy {
		t.Errorf("第三个应为 healthy, got %s", services[2].Status)
	}
}

func TestSeverityRank(t *testing.T) {
	cases := []struct {
		status   HealthStatus
		expected int
	}{
		{StatusCritical, 3},
		{StatusWarning, 2},
		{StatusUnknown, 1},
		{StatusHealthy, 0},
	}

	for _, c := range cases {
		rank := severityRank(c.status)
		if rank != c.expected {
			t.Errorf("%s 排名应为 %d, got %d", c.status, c.expected, rank)
		}
	}
}

// ========== 缓存测试 ==========

func TestCache(t *testing.T) {
	metrics := &fakeHealthMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service": {Service: "user-service", Healthy: true, CPUUsage: 30.0},
		},
		snapshot: AllMetricsSnapshot{},
	}
	alerts := &fakeHealthAlerts{}
	svc := NewHealthCardService(metrics, alerts, nil)
	svc.SetServices([]string{"user-service"})
	svc.cacheTTL = 1 * time.Minute

	// 第一次查询
	overview1, err := svc.GetOverview(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	// 修改指标值
	metrics.healthMap["user-service"] = ServiceHealth{Service: "user-service", Healthy: true, CPUUsage: 50.0}

	// 第二次查询（应走缓存，CPU 还是 30）
	overview2, err := svc.GetOverview(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	if overview2.Services[0].CPUUsage != 30.0 {
		t.Error("应走缓存返回旧值")
	}

	// 强制刷新
	overview3, err := svc.Refresh(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	if overview3.Services[0].CPUUsage != 50.0 {
		t.Error("刷新后应返回新值")
	}

	_ = overview1
}

// ========== 事件推送测试 ==========

func TestPushHealthStatus(t *testing.T) {
	metrics := &fakeHealthMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service": {Service: "user-service", Healthy: true, CPUUsage: 30.0},
		},
		snapshot: AllMetricsSnapshot{},
	}
	alerts := &fakeHealthAlerts{}
	publisher := &fakeHealthPublisher{}
	svc := NewHealthCardService(metrics, alerts, publisher)
	svc.SetServices([]string{"user-service"})

	svc.pushHealthStatus(context.Background(), "sandbox-123")

	if !publisher.pushed {
		t.Error("应推送事件")
	}
	if publisher.channel != "sandbox:sandbox-123" {
		t.Errorf("频道应为 sandbox:sandbox-123, got %s", publisher.channel)
	}
	if publisher.eventType != "health_status" {
		t.Errorf("事件类型应为 health_status, got %s", publisher.eventType)
	}
}

// ========== 单服务查询测试 ==========

func TestGetServiceCard(t *testing.T) {
	metrics := &fakeHealthMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":  {Service: "user-service", Healthy: true, CPUUsage: 30.0},
			"order-service": {Service: "order-service", Healthy: false, CPUUsage: 95.0, Reason: "CPU 过高"},
		},
		snapshot: AllMetricsSnapshot{},
	}
	alerts := &fakeHealthAlerts{
		alertMap: map[string]AlertCount{
			"order-service": {Total: 1, Critical: 1},
		},
	}
	svc := NewHealthCardService(metrics, alerts, nil)
	svc.SetServices([]string{"user-service", "order-service"})

	card, err := svc.GetServiceCard(context.Background(), "sandbox-123", "user-service")
	if err != nil {
		t.Fatal(err)
	}

	if card.Service != "user-service" {
		t.Errorf("服务名应为 user-service, got %s", card.Service)
	}
	if card.Status != StatusHealthy {
		t.Errorf("状态应为 healthy, got %s", card.Status)
	}
}

func TestGetServiceCard_NotFound(t *testing.T) {
	metrics := &fakeHealthMetrics{}
	alerts := &fakeHealthAlerts{}
	svc := NewHealthCardService(metrics, alerts, nil)
	svc.SetServices([]string{"user-service"})

	_, err := svc.GetServiceCard(context.Background(), "sandbox-123", "nonexistent")
	if err == nil {
		t.Error("不存在的服务应返回错误")
	}
}
