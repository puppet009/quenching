// frontend_event 事件驱动告警单元测试（第三批）。
//
// 覆盖：首事件立即触发与抑制、RecoveryWindow 静默自动恢复、
// 恢复后新事件重新告警、pending 静默回到 OK 不推送、事件类型过滤、
// frontend_event 跳过 pull 评估、默认规则幂等注册。
package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// newFrontendManager 构造带计数 publisher 的管理器（复用 alert_engine_test.go 的 fake 类型）。
func newFrontendManager() (*AlertManager, *fakePublisher) {
	pub := &fakePublisher{}
	mgr := NewAlertManager(
		NewAlertCompositeEvaluator(&fakeMetricsSource{}, &fakeLogSource{}),
		pub,
	)
	return mgr, pub
}

func frontendRule(id string, types []string, forDuration, recovery time.Duration) *AlertRule {
	return &AlertRule{
		ID:             id,
		Name:           "前端异常",
		Type:           AlertRuleTypeFrontendEvent,
		AlertSeverity:  AlertSeverityCritical,
		Enabled:        true,
		SandboxID:      "sb-1",
		EventTypes:     types,
		ForDuration:    forDuration,
		RecoveryWindow: recovery,
	}
}

func TestManager_ReportEvent_NonpositiveDurationIgnoresTimestampSkew(t *testing.T) {
	eventAt := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, initial := range []AlertStatus{AlertStatusOK, AlertStatusPending, AlertStatusResolved} {
		for _, duration := range []time.Duration{0, -time.Second} {
			t.Run(fmt.Sprintf("%s/%s", initial, duration), func(t *testing.T) {
				mgr, pub := newFrontendManager()
				rule := frontendRule("clock-skew", nil, duration, time.Minute)
				mgr.AddRule(rule)
				alert := &AlertInstance{
					ID: generateAlertID(rule), RuleID: rule.ID, Status: initial,
					StartedAt: eventAt.Add(time.Hour),
				}
				mgr.alerts[alert.ID] = alert
				mgr.ReportEvent(context.Background(), &FrontendEvent{
					SandboxID: "sb-1", Type: FrontendSignalPageError, EventAt: eventAt,
				})
				if alert.Status != AlertStatusFiring || pub.publishCount != 1 {
					t.Fatalf("nonpositive duration must fire despite earlier event time: status=%s notifications=%d", alert.Status, pub.publishCount)
				}
			})
		}
	}
}

func TestManager_ReportEvent_PositiveDurationWaitsUntilBoundary(t *testing.T) {
	mgr, pub := newFrontendManager()
	rule := frontendRule("positive-duration", nil, time.Minute, 2*time.Minute)
	mgr.AddRule(rule)
	event := &FrontendEvent{
		SandboxID: "sb-1", Type: FrontendSignalPageError,
		EventAt: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	mgr.ReportEvent(context.Background(), event)
	alert, _ := mgr.GetAlert(generateAlertID(rule))
	if alert.Status != AlertStatusPending || pub.publishCount != 0 {
		t.Fatalf("positive duration must start pending: status=%s notifications=%d", alert.Status, pub.publishCount)
	}
	event.EventAt = alert.StartedAt.Add(rule.ForDuration - time.Nanosecond)
	mgr.ReportEvent(context.Background(), event)
	if alert.Status != AlertStatusPending || pub.publishCount != 0 {
		t.Fatalf("must remain pending before duration boundary: status=%s notifications=%d", alert.Status, pub.publishCount)
	}
	event.EventAt = alert.StartedAt.Add(rule.ForDuration)
	mgr.ReportEvent(context.Background(), event)
	if alert.Status != AlertStatusFiring || pub.publishCount != 1 {
		t.Fatalf("must fire at duration boundary: status=%s notifications=%d", alert.Status, pub.publishCount)
	}
}

// 首事件立即触发，firing 中后续事件被抑制（仅刷新 LastEventAt，不重复推送）。
func TestManager_ReportEvent_ImmediateFiringAndSuppress(t *testing.T) {
	ctx := context.Background()
	mgr, pub := newFrontendManager()
	rule := frontendRule("fe-1", []string{FrontendSignalPageError}, 0, time.Second)
	mgr.AddRule(rule)

	mgr.ReportEvent(ctx, &FrontendEvent{
		SandboxID: "sb-1",
		Type:      FrontendSignalPageError,
		Step:      "下单",
		Message:   "Uncaught TypeError",
		EventAt:   time.Now(),
	})

	alert, ok := mgr.GetAlert(generateAlertID(rule))
	if !ok {
		t.Fatal("应创建告警实例")
	}
	if alert.Status != AlertStatusFiring {
		t.Fatalf("ForDuration=0 首事件后应为 firing, got %s", alert.Status)
	}
	if alert.Labels["source"] != AlertSourceFrontend {
		t.Errorf("labels.source 应为 frontend, got %q", alert.Labels["source"])
	}
	if alert.Labels["step"] != "下单" {
		t.Errorf("labels.step 应为 下单, got %q", alert.Labels["step"])
	}
	if pub.publishCount != 1 {
		t.Fatalf("首事件应推送 1 次, got %d", pub.publishCount)
	}

	// firing 中再来同类事件：抑制，不重复推送。
	second := time.Now()
	mgr.ReportEvent(ctx, &FrontendEvent{
		SandboxID: "sb-1",
		Type:      FrontendSignalPageError,
		Step:      "查询",
		EventAt:   second,
	})
	if pub.publishCount != 1 {
		t.Errorf("firing 中事件应被抑制，推送数仍为 1, got %d", pub.publishCount)
	}
	if !alert.LastEventAt.Equal(second) {
		t.Errorf("LastEventAt 应刷新为第二次事件时间, got %v", alert.LastEventAt)
	}
	if alert.Labels["step"] != "查询" {
		t.Errorf("labels.step 应更新为 查询, got %q", alert.Labels["step"])
	}
}

// 窗口内静默不恢复；超过 RecoveryWindow 静默后自动 resolved；新事件重新告警。
func TestManager_ReportEvent_SilenceResolveAndRefire(t *testing.T) {
	ctx := context.Background()
	mgr, pub := newFrontendManager()
	rule := frontendRule("fe-2", []string{FrontendSignalHTTPError}, 0, 250*time.Millisecond)
	mgr.AddRule(rule)

	mgr.ReportEvent(ctx, &FrontendEvent{
		SandboxID: "sb-1",
		Type:      FrontendSignalHTTPError,
		URL:       "/api/orders",
		Status:    500,
		EventAt:   time.Now(),
	})
	if pub.publishCount != 1 {
		t.Fatalf("首事件应推送 1 次, got %d", pub.publishCount)
	}

	// 窗口内：不恢复。
	mgr.checkFrontendSilence(ctx)
	if pub.publishCount != 1 {
		t.Errorf("窗口内不应恢复, got %d 次推送", pub.publishCount)
	}

	// 把最近事件时间拨到窗口之外：自动恢复。
	alert, _ := mgr.GetAlert(generateAlertID(rule))
	alert.LastEventAt = time.Now().Add(-time.Second)
	mgr.checkFrontendSilence(ctx)
	if alert.Status != AlertStatusResolved {
		t.Fatalf("超窗口静默后应为 resolved, got %s", alert.Status)
	}
	if pub.publishCount != 2 {
		t.Errorf("自动恢复应推送第 2 次, got %d", pub.publishCount)
	}

	// 恢复后再次出错：当作新告警重新触发。
	mgr.ReportEvent(ctx, &FrontendEvent{
		SandboxID: "sb-1",
		Type:      FrontendSignalHTTPError,
		URL:       "/api/orders",
		Status:    502,
		EventAt:   time.Now(),
	})
	if alert.Status != AlertStatusFiring {
		t.Fatalf("恢复后新事件应重新 firing, got %s", alert.Status)
	}
	if pub.publishCount != 3 {
		t.Errorf("重新告警应推送第 3 次, got %d", pub.publishCount)
	}
}

// 尚未越过 ForDuration（pending）即静默：回到 OK 且不推送恢复事件。
func TestManager_ReportEvent_PendingSilentBackToOK(t *testing.T) {
	ctx := context.Background()
	mgr, pub := newFrontendManager()
	rule := frontendRule("fe-3", []string{FrontendSignalStepFailure}, 10*time.Minute, 250*time.Millisecond)
	mgr.AddRule(rule)

	mgr.ReportEvent(ctx, &FrontendEvent{
		SandboxID: "sb-1",
		Type:      FrontendSignalStepFailure,
		Step:      "提交订单",
		EventAt:   time.Now(),
	})
	alert, _ := mgr.GetAlert(generateAlertID(rule))
	if alert.Status != AlertStatusPending {
		t.Fatalf("ForDuration 未满足应为 pending, got %s", alert.Status)
	}
	if pub.publishCount != 0 {
		t.Errorf("pending 不应推送, got %d", pub.publishCount)
	}

	alert.LastEventAt = time.Now().Add(-time.Second)
	mgr.checkFrontendSilence(ctx)
	if alert.Status != AlertStatusOK {
		t.Fatalf("pending 静默后应回到 OK, got %s", alert.Status)
	}
	if pub.publishCount != 0 {
		t.Errorf("未正式告警不应推送恢复事件, got %d", pub.publishCount)
	}
}

// 事件类型不在规则 EventTypes 中：忽略，不创建告警。
func TestManager_ReportEvent_EventTypeFilter(t *testing.T) {
	ctx := context.Background()
	mgr, pub := newFrontendManager()
	rule := frontendRule("fe-4", []string{FrontendSignalStepFailure}, 0, time.Second)
	mgr.AddRule(rule)

	mgr.ReportEvent(ctx, &FrontendEvent{
		SandboxID: "sb-1",
		Type:      FrontendSignalPageError,
		EventAt:   time.Now(),
	})
	if len(mgr.alerts) != 0 {
		t.Errorf("类型不匹配不应创建告警, got %d 个", len(mgr.alerts))
	}
	if pub.publishCount != 0 {
		t.Errorf("类型不匹配不应推送, got %d", pub.publishCount)
	}

	// 其他沙箱的事件同样忽略。
	mgr.ReportEvent(ctx, &FrontendEvent{
		SandboxID: "sb-OTHER",
		Type:      FrontendSignalStepFailure,
		EventAt:   time.Now(),
	})
	if len(mgr.alerts) != 0 {
		t.Errorf("其他沙箱事件不应创建告警, got %d 个", len(mgr.alerts))
	}
}

// frontend_event 规则跳过 pull 评估：EvaluateNow 不为其创建告警实例。
func TestManager_FrontendEvent_SkippedInPullEvaluation(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newFrontendManager()
	mgr.AddRule(frontendRule("fe-5", nil, 0, time.Second))

	mgr.EvaluateNow(ctx)
	if len(mgr.alerts) != 0 {
		t.Errorf("frontend_event 不应被 pull 评估, got %d 个告警", len(mgr.alerts))
	}
}

// 默认规则首次注册 11 条，重复注册幂等（返回 0，规则总数不变）。
func TestRegisterDefaults_Idempotent(t *testing.T) {
	mgr, _ := newFrontendManager()

	if got := RegisterDefaults(nil, "sb-1"); got != 0 {
		t.Errorf("nil 管理器应返回 0, got %d", got)
	}
	if got := RegisterDefaults(mgr, ""); got != 0 {
		t.Errorf("空 sandboxID 应返回 0, got %d", got)
	}

	first := RegisterDefaults(mgr, "sb-1")
	if first != 11 {
		t.Fatalf("首次应注册 11 条默认规则, got %d", first)
	}
	if len(mgr.ListRules()) != 11 {
		t.Errorf("规则总数应为 11, got %d", len(mgr.ListRules()))
	}

	second := RegisterDefaults(mgr, "sb-1")
	if second != 0 {
		t.Errorf("重复注册应返回 0, got %d", second)
	}
	if len(mgr.ListRules()) != 11 {
		t.Errorf("重复注册后规则总数应仍为 11, got %d", len(mgr.ListRules()))
	}

	// 不同沙箱互不影响，各自 11 条。
	other := RegisterDefaults(mgr, "sb-2")
	if other != 11 {
		t.Errorf("新沙箱应注册 11 条, got %d", other)
	}
	if len(mgr.ListRules()) != 22 {
		t.Errorf("两个沙箱规则总数应为 22, got %d", len(mgr.ListRules()))
	}
}
