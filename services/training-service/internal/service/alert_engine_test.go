// 告警规则引擎单元测试（T1-23）。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/pkg/eventbus"
)

// ========== 阈值评估测试 ==========

func TestEvaluateThreshold_GreaterThan(t *testing.T) {
	cases := []struct {
		value     float64
		threshold float64
		expected  bool
	}{
		{95.0, 90.0, true},
		{90.0, 90.0, false},
		{85.0, 90.0, false},
	}

	for _, c := range cases {
		result := evaluateThreshold(c.value, AlertOpGreaterThan, c.threshold)
		if result != c.expected {
			t.Errorf("%.2f > %.2f 期望 %v, got %v", c.value, c.threshold, c.expected, result)
		}
	}
}

func TestEvaluateThreshold_GreaterThanEqual(t *testing.T) {
	cases := []struct {
		value     float64
		threshold float64
		expected  bool
	}{
		{95.0, 90.0, true},
		{90.0, 90.0, true},
		{85.0, 90.0, false},
	}

	for _, c := range cases {
		result := evaluateThreshold(c.value, AlertOpGreaterThanEqual, c.threshold)
		if result != c.expected {
			t.Errorf("%.2f >= %.2f 期望 %v, got %v", c.value, c.threshold, c.expected, result)
		}
	}
}

func TestEvaluateThreshold_LessThan(t *testing.T) {
	result := evaluateThreshold(10.0, AlertOpLessThan, 20.0)
	if !result {
		t.Error("10 < 20 应为 true")
	}
}

func TestEvaluateThreshold_Equal(t *testing.T) {
	result := evaluateThreshold(10.0, AlertOpEqual, 10.0)
	if !result {
		t.Error("10 == 10 应为 true")
	}
}

// ========== 状态机测试 ==========

func TestAlertStateMachine_OKToFiring(t *testing.T) {
	alert := &AlertInstance{Status: AlertStatusOK}

	// 首次触发 → Pending
	changed := alert.transition(eventTriggered)
	if !changed || alert.Status != AlertStatusPending {
		t.Errorf("首次触发后应为 Pending, got %s", alert.Status)
	}
	if alert.StartedAt.IsZero() {
		t.Error("首次触发应设置 StartedAt")
	}

	// 持续时间满足 → Firing
	changed = alert.transition(eventForDurationMet)
	if !changed || alert.Status != AlertStatusFiring {
		t.Errorf("持续时间满足后应为 Firing, got %s", alert.Status)
	}
	if alert.FiredAt.IsZero() {
		t.Error("Firing 应设置 FiredAt")
	}
}

func TestAlertStateMachine_PendingRecover(t *testing.T) {
	alert := &AlertInstance{Status: AlertStatusPending}

	// 在持续时间内恢复 → OK
	changed := alert.transition(eventResolved)
	if !changed || alert.Status != AlertStatusOK {
		t.Errorf("Pending 恢复后应为 OK, got %s", alert.Status)
	}
}

func TestAlertStateMachine_FiringToResolved(t *testing.T) {
	alert := &AlertInstance{Status: AlertStatusFiring}

	// 告警中恢复 → Resolved
	changed := alert.transition(eventResolved)
	if !changed || alert.Status != AlertStatusResolved {
		t.Errorf("Firing 恢复后应为 Resolved, got %s", alert.Status)
	}
	if alert.ResolvedAt.IsZero() {
		t.Error("Resolved 应设置 ResolvedAt")
	}
}

func TestAlertStateMachine_Acknowledge(t *testing.T) {
	alert := &AlertInstance{Status: AlertStatusFiring}

	// 确认 → Acknowledged
	changed := alert.transition(eventAcknowledged)
	if !changed || alert.Status != AlertStatusAcknowledged {
		t.Errorf("确认后应为 Acknowledged, got %s", alert.Status)
	}
}

func TestAlertStateMachine_InvalidTransition(t *testing.T) {
	alert := &AlertInstance{Status: AlertStatusOK}

	// OK 状态下直接恢复 → 不变化
	changed := alert.transition(eventResolved)
	if changed {
		t.Error("OK 状态下 eventResolved 不应有变化")
	}
	if alert.Status != AlertStatusOK {
		t.Errorf("状态应仍为 OK, got %s", alert.Status)
	}
}

// ========== Mock 数据源 ==========

type fakeMetricsSource struct {
	currentValue float64
	rangePoints  []AlertDataPoint
}

func (f *fakeMetricsSource) GetCurrentValue(_ context.Context, _, _, _ string, _ string) (float64, error) {
	return f.currentValue, nil
}

func (f *fakeMetricsSource) QueryRange(_ context.Context, _, _, _ string, _, _ time.Time, _ int) ([]AlertDataPoint, error) {
	return f.rangePoints, nil
}

type fakeLogSource struct {
	count int
}

func (f *fakeLogSource) CountKeyword(_ context.Context, _, _, _, _ string, _, _ time.Time) (int, error) {
	return f.count, nil
}

type fakePublisher struct {
	publishCount int
}

func (f *fakePublisher) Publish(_ context.Context, _ eventbus.TargetType, _ string, _ eventbus.EventType, _ interface{}) error {
	f.publishCount++
	return nil
}

func (f *fakePublisher) PublishToChannel(_ context.Context, _ string, _ eventbus.EventType, _ interface{}) error {
	f.publishCount++
	return nil
}

func (f *fakePublisher) PublishToUser(_ context.Context, _ int64, _ eventbus.EventType, _ interface{}) error {
	f.publishCount++
	return nil
}

func (f *fakePublisher) Broadcast(_ context.Context, _ eventbus.EventType, _ interface{}) error {
	f.publishCount++
	return nil
}

// ========== 阈值评估器测试 ==========

func TestAlertThresholdEvaluator_Firing(t *testing.T) {
	fake := &fakeMetricsSource{currentValue: 95.0}
	evaluator := NewAlertThresholdEvaluator(fake)

	rule := &AlertRule{
		Type:          AlertRuleTypeThreshold,
		Metric:        "cpu_usage",
		AlertOperator: AlertOpGreaterThan,
		Threshold:     90.0,
		SandboxID:     "sandbox-123",
	}

	result, err := evaluator.Evaluate(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Fired {
		t.Error("CPU 95% > 90% 应触发告警")
	}
	if result.Value != 95.0 {
		t.Errorf("当前值应为 95.0, got %.2f", result.Value)
	}
}

func TestAlertThresholdEvaluator_NotFiring(t *testing.T) {
	fake := &fakeMetricsSource{currentValue: 80.0}
	evaluator := NewAlertThresholdEvaluator(fake)

	rule := &AlertRule{
		Type:          AlertRuleTypeThreshold,
		Metric:        "cpu_usage",
		AlertOperator: AlertOpGreaterThan,
		Threshold:     90.0,
		SandboxID:     "sandbox-123",
	}

	result, err := evaluator.Evaluate(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	if result.Fired {
		t.Error("CPU 80% > 90% 不应触发告警")
	}
}

// ========== 日志关键词评估器测试 ==========

func TestAlertLogKeywordEvaluator_Firing(t *testing.T) {
	fake := &fakeLogSource{count: 5}
	evaluator := NewAlertLogKeywordEvaluator(fake)
	evaluator.Threshold = 3

	rule := &AlertRule{
		Type:       AlertRuleTypeLogKeyword,
		LogKeyword: "OutOfMemoryError",
		LogLevel:   "ERROR",
		SandboxID:  "sandbox-123",
	}

	result, err := evaluator.Evaluate(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Fired {
		t.Error("出现 5 次关键词应触发告警")
	}
	if result.Value != 5 {
		t.Errorf("计数应为 5, got %.0f", result.Value)
	}
}

func TestAlertLogKeywordEvaluator_NotFiring(t *testing.T) {
	fake := &fakeLogSource{count: 0}
	evaluator := NewAlertLogKeywordEvaluator(fake)

	rule := &AlertRule{
		Type:       AlertRuleTypeLogKeyword,
		LogKeyword: "OutOfMemoryError",
		SandboxID:  "sandbox-123",
	}

	result, err := evaluator.Evaluate(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	if result.Fired {
		t.Error("没有关键词不应触发告警")
	}
}

// ========== 管理器测试 ==========

func TestManager_AddAndListRules(t *testing.T) {
	fakeMetrics := &fakeMetricsSource{currentValue: 50.0}
	fakeLogs := &fakeLogSource{}
	evaluator := NewAlertCompositeEvaluator(fakeMetrics, fakeLogs)
	publisher := &fakePublisher{}
	mgr := NewAlertManager(evaluator, publisher)

	rule := &AlertRule{
		ID:            "rule-1",
		Name:          "CPU 过高告警",
		Type:          AlertRuleTypeThreshold,
		AlertSeverity: AlertSeverityWarning,
		Enabled:       true,
		SandboxID:     "sandbox-123",
		Metric:        "cpu_usage",
		AlertOperator: AlertOpGreaterThan,
		Threshold:     90.0,
	}

	mgr.AddRule(rule)

	rules := mgr.ListRules()
	if len(rules) != 1 {
		t.Errorf("应有 1 条规则, got %d", len(rules))
	}

	retrieved, ok := mgr.GetRule("rule-1")
	if !ok {
		t.Fatal("应能找到 rule-1")
	}
	if retrieved.Name != "CPU 过高告警" {
		t.Errorf("规则名不匹配: %s", retrieved.Name)
	}
}

func TestManager_Evaluate_FiringAndResolve(t *testing.T) {
	fakeMetrics := &fakeMetricsSource{currentValue: 95.0}
	fakeLogs := &fakeLogSource{}
	evaluator := NewAlertCompositeEvaluator(fakeMetrics, fakeLogs)
	publisher := &fakePublisher{}
	mgr := NewAlertManager(evaluator, publisher)
	mgr.SetEvalInterval(1 * time.Millisecond) // 测试用，快速

	rule := &AlertRule{
		ID:            "rule-cpu",
		Name:          "CPU 过高告警",
		Type:          AlertRuleTypeThreshold,
		AlertSeverity: AlertSeverityCritical,
		Enabled:       true,
		SandboxID:     "sandbox-123",
		Service:       "user-service",
		Metric:        "cpu_usage",
		AlertOperator: AlertOpGreaterThan,
		Threshold:     90.0,
		ForDuration:   0, // 立即触发
	}

	mgr.AddRule(rule)

	// 第一次评估：触发告警
	mgr.EvaluateNow(context.Background())

	alerts := mgr.ListAlerts(AlertStatusFiring)
	if len(alerts) != 1 {
		t.Errorf("应有 1 条告警在 Firing 状态, got %d", len(alerts))
	}

	if publisher.publishCount != 1 {
		t.Errorf("应推送 1 次告警事件, got %d", publisher.publishCount)
	}

	// 指标恢复正常
	fakeMetrics.currentValue = 50.0

	// 第二次评估：告警恢复
	mgr.EvaluateNow(context.Background())

	resolvedAlerts := mgr.ListAlerts(AlertStatusResolved)
	if len(resolvedAlerts) != 1 {
		t.Errorf("应有 1 条告警已恢复, got %d", len(resolvedAlerts))
	}

	if publisher.publishCount != 2 {
		t.Errorf("应推送 2 次（触发 + 恢复）, got %d", publisher.publishCount)
	}
}

func TestManager_ForDuration(t *testing.T) {
	fakeMetrics := &fakeMetricsSource{currentValue: 95.0}
	fakeLogs := &fakeLogSource{}
	evaluator := NewAlertCompositeEvaluator(fakeMetrics, fakeLogs)
	publisher := &fakePublisher{}
	mgr := NewAlertManager(evaluator, publisher)

	rule := &AlertRule{
		ID:            "rule-cpu",
		Name:          "CPU 过高告警",
		Type:          AlertRuleTypeThreshold,
		AlertSeverity: AlertSeverityWarning,
		Enabled:       true,
		SandboxID:     "sandbox-123",
		Metric:        "cpu_usage",
		AlertOperator: AlertOpGreaterThan,
		Threshold:     90.0,
		ForDuration:   1 * time.Hour, // 1 小时持续时间，测试中不会满足
	}

	mgr.AddRule(rule)

	// 第一次评估：进入 Pending
	mgr.EvaluateNow(context.Background())

	pendingAlerts := mgr.ListAlerts(AlertStatusPending)
	if len(pendingAlerts) != 1 {
		t.Errorf("应有 1 条告警在 Pending 状态, got %d", len(pendingAlerts))
	}

	if publisher.publishCount != 0 {
		t.Errorf("Pending 状态不应推送告警, got %d", publisher.publishCount)
	}
}

func TestManager_Acknowledge(t *testing.T) {
	fakeMetrics := &fakeMetricsSource{currentValue: 95.0}
	fakeLogs := &fakeLogSource{}
	evaluator := NewAlertCompositeEvaluator(fakeMetrics, fakeLogs)
	publisher := &fakePublisher{}
	mgr := NewAlertManager(evaluator, publisher)

	rule := &AlertRule{
		ID:            "rule-cpu",
		Name:          "CPU 过高告警",
		Type:          AlertRuleTypeThreshold,
		AlertSeverity: AlertSeverityWarning,
		Enabled:       true,
		SandboxID:     "sandbox-123",
		Metric:        "cpu_usage",
		AlertOperator: AlertOpGreaterThan,
		Threshold:     90.0,
		ForDuration:   0,
	}

	mgr.AddRule(rule)
	mgr.EvaluateNow(context.Background())

	alerts := mgr.ListAlerts(AlertStatusFiring)
	if len(alerts) != 1 {
		t.Fatal("应先有 firing 告警")
	}

	alertID := alerts[0].ID
	success := mgr.AcknowledgeAlert(alertID)
	if !success {
		t.Error("确认告警应成功")
	}

	ackAlerts := mgr.ListAlerts(AlertStatusAcknowledged)
	if len(ackAlerts) != 1 {
		t.Errorf("应有 1 条已确认告警, got %d", len(ackAlerts))
	}
}

func TestManager_RemoveRule(t *testing.T) {
	fakeMetrics := &fakeMetricsSource{}
	fakeLogs := &fakeLogSource{}
	evaluator := NewAlertCompositeEvaluator(fakeMetrics, fakeLogs)
	mgr := NewAlertManager(evaluator, nil)

	mgr.AddRule(&AlertRule{ID: "rule-1", Name: "测试规则"})
	mgr.RemoveRule("rule-1")

	if len(mgr.ListRules()) != 0 {
		t.Error("删除后规则列表应为空")
	}
}

// ========== 告警ID生成测试 ==========

func TestGenerateAlertID(t *testing.T) {
	rule1 := &AlertRule{
		ID:        "rule-1",
		SandboxID: "sandbox-123",
		Service:   "user-service",
	}

	rule2 := &AlertRule{
		ID:        "rule-1",
		SandboxID: "sandbox-123",
		Service:   "user-service",
	}

	rule3 := &AlertRule{
		ID:        "rule-1",
		SandboxID: "sandbox-456",
		Service:   "user-service",
	}

	id1 := generateAlertID(rule1)
	id2 := generateAlertID(rule2)
	id3 := generateAlertID(rule3)

	if id1 != id2 {
		t.Error("相同规则应生成相同告警ID")
	}
	if id1 == id3 {
		t.Error("不同沙箱应生成不同告警ID")
	}
	if len(id1) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("告警ID长度应为 16, got %d", len(id1))
	}
}

// ========== 工具函数测试 ==========

func TestAveragePoints(t *testing.T) {
	points := []AlertDataPoint{
		{Value: 10},
		{Value: 20},
		{Value: 30},
	}

	avg := averagePoints(points)
	if avg != 20.0 {
		t.Errorf("平均值应为 20, got %.2f", avg)
	}
}

func TestAveragePoints_Empty(t *testing.T) {
	avg := averagePoints(nil)
	if avg != 0 {
		t.Errorf("空数据平均值应为 0, got %.2f", avg)
	}
}

func TestContainsKeyword(t *testing.T) {
	cases := []struct {
		text     string
		keyword  string
		expected bool
	}{
		{"Error occurred", "error", true},
		{"ERROR occurred", "error", true},
		{"everything ok", "error", false},
		{"", "error", false},
	}

	for _, c := range cases {
		result := containsKeyword(c.text, c.keyword)
		if result != c.expected {
			t.Errorf("'%s' contains '%s' 期望 %v, got %v",
				c.text, c.keyword, c.expected, result)
		}
	}
}
