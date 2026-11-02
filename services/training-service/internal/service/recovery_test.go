// 故障恢复验证单元测试（T1-29）。
package service

import (
	"context"
	"testing"
	"time"
)

// ========== Mock 数据 ==========

type fakeRecoveryMetrics struct {
	healthMap map[string]ServiceHealth
}

func (f *fakeRecoveryMetrics) GetServiceHealth(service string) (ServiceHealth, error) {
	if h, ok := f.healthMap[service]; ok {
		return h, nil
	}
	return ServiceHealth{Service: service, Healthy: true}, nil
}

func (f *fakeRecoveryMetrics) GetMetricValue(service, metric string) (float64, error) {
	return 0, nil
}

type fakeRecoveryAlerts struct {
	total    int
	critical int
	warning  int
}

func (f *fakeRecoveryAlerts) GetActiveAlertCount(_ string) (int, int, int) {
	return f.total, f.critical, f.warning
}

type fakeLogs struct {
	errorCount int
	err        error
}

func (f *fakeLogs) CountErrors(_ string, _ int) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.errorCount, nil
}

type fakeProbe struct {
	results map[string]bool
}

func (f *fakeProbe) CheckEndpoint(url string, _ time.Duration) (bool, int, error) {
	if ok, exists := f.results[url]; exists && ok {
		return true, 200, nil
	}
	return false, 500, nil
}

// ========== 指标检查器测试 ==========

func TestMetricsChecker_AllHealthy(t *testing.T) {
	fake := &fakeRecoveryMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":  {Service: "user-service", Healthy: true, CPUUsage: 30, MemoryUsage: 40, ErrorRate: 0.5},
			"order-service": {Service: "order-service", Healthy: true, CPUUsage: 50, MemoryUsage: 60, ErrorRate: 1.0},
		},
	}

	checker := NewMetricsChecker(fake, []string{"user-service", "order-service"})

	if checker.Category() != CategoryMetrics {
		t.Errorf("类别应为 metrics, got %s", checker.Category())
	}

	items, err := checker.Check(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 2 {
		t.Errorf("应有 2 个检查项, got %d", len(items))
	}

	for _, item := range items {
		if item.Status != StatusPassed {
			t.Errorf("%s 应为 passed, got %s", item.Name, item.Status)
		}
	}
}

func TestMetricsChecker_Unhealthy(t *testing.T) {
	fake := &fakeRecoveryMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":  {Service: "user-service", Healthy: true},
			"order-service": {Service: "order-service", Healthy: false, Reason: "CPU 过高"},
		},
	}

	checker := NewMetricsChecker(fake, []string{"user-service", "order-service"})

	items, err := checker.Check(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	passed := 0
	failed := 0
	for _, item := range items {
		if item.Status == StatusPassed {
			passed++
		} else if item.Status == StatusFailed {
			failed++
		}
	}

	if passed != 1 {
		t.Errorf("应有 1 个通过, got %d", passed)
	}
	if failed != 1 {
		t.Errorf("应有 1 个失败, got %d", failed)
	}
}

// ========== 告警检查器测试 ==========

func TestAlertChecker_NoAlerts(t *testing.T) {
	fake := &fakeRecoveryAlerts{total: 0, critical: 0, warning: 0}
	checker := NewAlertChecker(fake)

	items, err := checker.Check(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 2 {
		t.Errorf("应有 2 个检查项, got %d", len(items))
	}

	for _, item := range items {
		if item.Status != StatusPassed {
			t.Errorf("%s 应为 passed, got %s", item.Name, item.Status)
		}
	}
}

func TestAlertChecker_HasCritical(t *testing.T) {
	fake := &fakeRecoveryAlerts{total: 3, critical: 1, warning: 2}
	checker := NewAlertChecker(fake)

	items, err := checker.Check(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	criticalItem := items[0]
	if criticalItem.Status != StatusFailed {
		t.Errorf("有 critical 告警时应失败, got %s", criticalItem.Status)
	}

	warningItem := items[1]
	if warningItem.Status != StatusFailed {
		t.Errorf("有 warning 告警时应失败, got %s", warningItem.Status)
	}
}

func TestAlertChecker_OnlyWarning(t *testing.T) {
	fake := &fakeRecoveryAlerts{total: 2, critical: 0, warning: 2}
	checker := NewAlertChecker(fake)

	items, _ := checker.Check(context.Background(), "sandbox-123")

	if items[0].Status != StatusPassed {
		t.Error("无 critical 告警应通过")
	}
	if items[1].Status != StatusFailed {
		t.Error("有 warning 告警应失败")
	}
}

// ========== 日志检查器测试 ==========

func TestLogChecker_LowErrors(t *testing.T) {
	fake := &fakeLogs{errorCount: 2}
	checker := NewLogChecker(fake)
	checker.errorThreshold = 5
	checker.minutes = 5

	items, err := checker.Check(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Errorf("应有 1 个检查项, got %d", len(items))
	}

	if items[0].Status != StatusPassed {
		t.Errorf("错误数低于阈值应通过, got %s", items[0].Status)
	}
}

func TestLogChecker_HighErrors(t *testing.T) {
	fake := &fakeLogs{errorCount: 10}
	checker := NewLogChecker(fake)
	checker.errorThreshold = 5

	items, _ := checker.Check(context.Background(), "sandbox-123")

	if items[0].Status != StatusFailed {
		t.Errorf("错误数高于阈值应失败, got %s", items[0].Status)
	}
}

// ========== 健康检查器测试 ==========

func TestHealthChecker_AllHealthy(t *testing.T) {
	fake := &fakeRecoveryMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":    {Healthy: true},
			"product-service": {Healthy: true},
			"order-service":   {Healthy: true},
		},
	}

	checker := NewHealthChecker(fake, []string{"user-service", "product-service", "order-service"})

	items, err := checker.Check(context.Background(), "sandbox-123")
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Errorf("应有 1 个检查项, got %d", len(items))
	}

	if items[0].Status != StatusPassed {
		t.Errorf("所有服务健康应通过, got %s", items[0].Status)
	}
}

func TestHealthChecker_SomeUnhealthy(t *testing.T) {
	fake := &fakeRecoveryMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":  {Healthy: true},
			"order-service": {Healthy: false},
		},
	}

	checker := NewHealthChecker(fake, []string{"user-service", "order-service"})

	items, _ := checker.Check(context.Background(), "sandbox-123")

	if items[0].Status != StatusFailed {
		t.Errorf("有服务不健康应失败, got %s", items[0].Status)
	}
}

// ========== 接口探活检查器测试 ==========

func TestProbeChecker_AllPass(t *testing.T) {
	fake := &fakeProbe{
		results: map[string]bool{
			"http://user/health":  true,
			"http://order/health": true,
		},
	}

	endpoints := []ProbeEndpoint{
		{ID: "user", Name: "用户服务", URL: "http://user/health", Weight: 5, Timeout: 5 * time.Second},
		{ID: "order", Name: "订单服务", URL: "http://order/health", Weight: 5, Timeout: 5 * time.Second},
	}

	checker := NewProbeChecker(fake, endpoints)
	items, _ := checker.Check(context.Background(), "sandbox-123")

	for _, item := range items {
		if item.Status != StatusPassed {
			t.Errorf("%s 探活应通过, got %s", item.Name, item.Status)
		}
	}
}

func TestProbeChecker_SomeFail(t *testing.T) {
	fake := &fakeProbe{
		results: map[string]bool{
			"http://user/health":  true,
			"http://order/health": false,
		},
	}

	endpoints := []ProbeEndpoint{
		{ID: "user", Name: "用户服务", URL: "http://user/health", Weight: 5},
		{ID: "order", Name: "订单服务", URL: "http://order/health", Weight: 5},
	}

	checker := NewProbeChecker(fake, endpoints)
	items, _ := checker.Check(context.Background(), "sandbox-123")

	if items[0].Status != StatusPassed {
		t.Error("用户服务探活应通过")
	}
	if items[1].Status != StatusFailed {
		t.Error("订单服务探活应失败")
	}
}

// ========== 恢复验证服务测试 ==========

func TestService_Verify_AllPass(t *testing.T) {
	svc := NewRecoveryService()

	// 注册指标检查器（全健康）
	metricsChecker := NewMetricsChecker(&fakeRecoveryMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":  {Healthy: true},
			"order-service": {Healthy: true},
		},
	}, []string{"user-service", "order-service"})
	svc.RegisterChecker(metricsChecker)

	// 注册告警检查器（无告警）
	alertChecker := NewAlertChecker(&fakeRecoveryAlerts{})
	svc.RegisterChecker(alertChecker)

	result, err := svc.Verify(context.Background(), "sandbox-123", "sess-1")
	if err != nil {
		t.Fatal(err)
	}

	if result.OverallStatus != StatusPassed {
		t.Errorf("全部通过时总体状态应为 passed, got %s", result.OverallStatus)
	}
	if result.Score != 100.0 {
		t.Errorf("全部通过时得分应为 100, got %.2f", result.Score)
	}
	if result.FailedCount != 0 {
		t.Errorf("失败数应为 0, got %d", result.FailedCount)
	}
}

func TestService_Verify_PartialPass(t *testing.T) {
	svc := NewRecoveryService()

	// 指标：1 个通过，1 个失败（各 10 分权重）
	metricsChecker := NewMetricsChecker(&fakeRecoveryMetrics{
		healthMap: map[string]ServiceHealth{
			"user-service":  {Healthy: true},
			"order-service": {Healthy: false, Reason: "CPU 过高"},
		},
	}, []string{"user-service", "order-service"})
	svc.RegisterChecker(metricsChecker)

	// 告警：无告警（20+10=30 分权重）
	alertChecker := NewAlertChecker(&fakeRecoveryAlerts{})
	svc.RegisterChecker(alertChecker)

	// 总权重：10+10+20+10 = 50
	// 通过权重：10(user) + 20(no critical) + 10(no warning) = 40
	// 得分：40/50 * 100 = 80

	result, err := svc.Verify(context.Background(), "sandbox-123", "sess-2")
	if err != nil {
		t.Fatal(err)
	}

	if result.OverallStatus != StatusFailed {
		t.Errorf("有失败项时总体状态应为 failed, got %s", result.OverallStatus)
	}
	if result.Score != 80.0 {
		t.Errorf("得分应为 80, got %.2f", result.Score)
	}
	if result.PassedCount != 3 {
		t.Errorf("通过数应为 3, got %d", result.PassedCount)
	}
	if result.FailedCount != 1 {
		t.Errorf("失败数应为 1, got %d", result.FailedCount)
	}
}

func TestService_GetLastResult(t *testing.T) {
	svc := NewRecoveryService()

	// 先验证一次
	svc.RegisterChecker(NewAlertChecker(&fakeRecoveryAlerts{}))
	_, err := svc.Verify(context.Background(), "sandbox-123", "sess-1")
	if err != nil {
		t.Fatal(err)
	}

	// 获取最近结果
	result, exists := svc.GetLastResult("sess-1")
	if !exists {
		t.Fatal("应能找到最近结果")
	}
	if result.SessionID != "sess-1" {
		t.Errorf("会话ID不匹配")
	}

	// 不存在的会话
	_, exists = svc.GetLastResult("nonexistent")
	if exists {
		t.Error("不存在的会话不应有结果")
	}
}

func TestService_Duration(t *testing.T) {
	svc := NewRecoveryService()
	svc.RegisterChecker(NewAlertChecker(&fakeRecoveryAlerts{}))

	result, _ := svc.Verify(context.Background(), "sandbox-123", "sess-1")

	if result.StartTime == 0 {
		t.Error("开始时间不应为 0")
	}
	if result.EndTime == 0 {
		t.Error("结束时间不应为 0")
	}
	if result.EndTime < result.StartTime {
		t.Error("结束时间应大于开始时间")
	}
}

// ========== 得分计算测试 ==========

func TestCalculateResult_AllPassed(t *testing.T) {
	svc := NewRecoveryService()
	result := &VerificationResult{
		Checks: []CheckItem{
			{ID: "a", Weight: 30, Status: StatusPassed, Enabled: true},
			{ID: "b", Weight: 40, Status: StatusPassed, Enabled: true},
			{ID: "c", Weight: 30, Status: StatusPassed, Enabled: true},
		},
	}

	svc.calculateResult(result)

	if result.Score != 100.0 {
		t.Errorf("全部通过得分应为 100, got %.2f", result.Score)
	}
	if result.OverallStatus != StatusPassed {
		t.Errorf("全部通过状态应为 passed")
	}
}

func TestCalculateResult_SomeFailed(t *testing.T) {
	svc := NewRecoveryService()
	result := &VerificationResult{
		Checks: []CheckItem{
			{ID: "a", Weight: 30, Status: StatusPassed, Enabled: true},
			{ID: "b", Weight: 40, Status: StatusFailed, Enabled: true},
			{ID: "c", Weight: 30, Status: StatusPassed, Enabled: true},
		},
	}

	svc.calculateResult(result)

	// 通过权重：30+30=60, 总权重：100, 得分：60
	if result.Score != 60.0 {
		t.Errorf("得分应为 60, got %.2f", result.Score)
	}
	if result.OverallStatus != StatusFailed {
		t.Errorf("有失败项状态应为 failed")
	}
}

func TestCalculateResult_WithSkipped(t *testing.T) {
	svc := NewRecoveryService()
	result := &VerificationResult{
		Checks: []CheckItem{
			{ID: "a", Weight: 50, Status: StatusPassed, Enabled: true},
			{ID: "b", Weight: 50, Status: StatusSkipped, Enabled: false},
		},
	}

	svc.calculateResult(result)

	// 跳过的不计入权重，总权重 50，通过 50，得分 100
	if result.Score != 100.0 {
		t.Errorf("跳过项不计入，得分应为 100, got %.2f", result.Score)
	}
	if result.SkippedCount != 1 {
		t.Errorf("跳过数应为 1, got %d", result.SkippedCount)
	}
}

// ========== 检查状态判断 ==========

func TestCheckStatus_Values(t *testing.T) {
	if string(StatusPassed) != "passed" {
		t.Error("StatusPassed 应为 passed")
	}
	if string(StatusFailed) != "failed" {
		t.Error("StatusFailed 应为 failed")
	}
	if string(StatusSkipped) != "skipped" {
		t.Error("StatusSkipped 应为 skipped")
	}
	if string(StatusPending) != "pending" {
		t.Error("StatusPending 应为 pending")
	}
}

// ========== 检查类别测试 ==========

func TestCheckCategory_Values(t *testing.T) {
	categories := []CheckCategory{
		CategoryMetrics,
		CategoryAlerts,
		CategoryLogs,
		CategoryHealth,
		CategoryProbe,
		CategoryResource,
	}

	if len(categories) != 6 {
		t.Errorf("应有 6 个检查类别, got %d", len(categories))
	}
}
