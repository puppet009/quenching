// 告警管理器（T1-23）。
//
// 负责管理告警规则、定时评估、状态机驱动、去重抑制、事件推送。
package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/quenching/training-service/internal/pkg/eventbus"
)

// defaultFrontendRecoveryWindow 前端事件型规则默认自动恢复窗口。
const defaultFrontendRecoveryWindow = 60 * time.Second

// AlertEventHandler 告警事件处理器接口。
//
// 当告警状态变化时调用，用于推送事件、落库等。
type AlertEventHandler interface {
	OnAlertFiring(ctx context.Context, alert *AlertInstance)
	OnAlertResolved(ctx context.Context, alert *AlertInstance)
}

// AlertManager 告警管理器。
//
// 功能：
//   - 注册/删除告警规则
//   - 定时评估所有规则
//   - 维护告警状态机
//   - 去重与抑制
//   - 推送告警事件
type AlertManager struct {
	evaluator AlertEvaluator
	publisher eventbus.Publisher
	handler   AlertEventHandler

	rules  map[string]*AlertRule     // ruleID -> rule
	alerts map[string]*AlertInstance // alertID -> alert

	mu sync.RWMutex

	// 评估间隔
	evalInterval time.Duration

	// 告警保留时间（resolved 状态保留多久后清理）
	retentionPeriod time.Duration

	// 停止通道
	stopChan chan struct{}
}

// NewAlertManager 创建告警管理器。
func NewAlertManager(evaluator AlertEvaluator, publisher eventbus.Publisher) *AlertManager {
	return &AlertManager{
		evaluator:       evaluator,
		publisher:       publisher,
		rules:           make(map[string]*AlertRule),
		alerts:          make(map[string]*AlertInstance),
		evalInterval:    30 * time.Second,
		retentionPeriod: 24 * time.Hour,
		stopChan:        make(chan struct{}),
	}
}

// SetHandler 设置告警事件处理器。
func (m *AlertManager) SetHandler(h AlertEventHandler) {
	m.handler = h
}

// SetEvalInterval 设置评估间隔。
func (m *AlertManager) SetEvalInterval(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evalInterval = d
}

// AddRule 添加告警规则。
func (m *AlertManager) AddRule(rule *AlertRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[rule.ID] = rule
}

// RemoveRule 删除告警规则。
func (m *AlertManager) RemoveRule(ruleID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rules, ruleID)
}

// GetRule 获取指定规则。
func (m *AlertManager) GetRule(ruleID string) (*AlertRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rule, ok := m.rules[ruleID]
	return rule, ok
}

// ListRules 列出所有规则。
func (m *AlertManager) ListRules() []*AlertRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rules := make([]*AlertRule, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	return rules
}

// ListAlerts 列出所有告警（按状态过滤）。
//
// status 为空时返回所有告警。
func (m *AlertManager) ListAlerts(status AlertStatus) []*AlertInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alerts := make([]*AlertInstance, 0, len(m.alerts))
	for _, a := range m.alerts {
		if status == "" || a.Status == status {
			alerts = append(alerts, a)
		}
	}
	return alerts
}

// GetAlert 获取指定告警。
func (m *AlertManager) GetAlert(alertID string) (*AlertInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	alert, ok := m.alerts[alertID]
	return alert, ok
}

// AcknowledgeAlert 确认告警。
func (m *AlertManager) AcknowledgeAlert(alertID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	alert, ok := m.alerts[alertID]
	if !ok {
		return false
	}

	changed := alert.transition(eventAcknowledged)
	if changed {
		// 确认后不推送事件（静默处理）
	}
	return changed
}

// Start 启动告警管理器（定时评估）。
func (m *AlertManager) Start(ctx context.Context) {
	ticker := time.NewTicker(m.evalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.evaluateAll(ctx)
			m.checkFrontendSilence(ctx)
			m.cleanupResolved()
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		}
	}
}

// Stop 停止告警管理器。
func (m *AlertManager) Stop() {
	close(m.stopChan)
}

// EvaluateNow 立即触发一次评估（用于测试或手动触发）。
func (m *AlertManager) EvaluateNow(ctx context.Context) {
	m.evaluateAll(ctx)
}

// evaluateAll 评估所有启用的规则。
func (m *AlertManager) evaluateAll(ctx context.Context) {
	m.mu.RLock()
	rules := make([]*AlertRule, 0, len(m.rules))
	for _, r := range m.rules {
		// frontend_event 为事件驱动 push 型，不做 pull 评估
		if r.Enabled && r.Type != AlertRuleTypeFrontendEvent {
			rules = append(rules, r)
		}
	}
	m.mu.RUnlock()

	for _, rule := range rules {
		m.evaluateRule(ctx, rule)
	}
}

// evaluateRule 评估单条规则。
func (m *AlertManager) evaluateRule(ctx context.Context, rule *AlertRule) {
	result, err := m.evaluator.Evaluate(ctx, rule)
	if err != nil {
		// 评估失败不影响其他规则
		return
	}

	alertID := generateAlertID(rule)

	m.mu.Lock()
	defer m.mu.Unlock()

	alert, exists := m.alerts[alertID]

	if !exists {
		// 新告警
		alert = &AlertInstance{
			ID:            alertID,
			RuleID:        rule.ID,
			RuleName:      rule.Name,
			AlertSeverity: rule.AlertSeverity,
			Status:        AlertStatusOK,
			SandboxID:     rule.SandboxID,
			Service:       rule.Service,
			Metric:        rule.Metric,
			Threshold:     rule.Threshold,
			Labels:        rule.Labels,
		}
		m.alerts[alertID] = alert
	}

	// 更新当前值
	alert.Value = result.Value

	if result.Fired {
		// 条件满足
		switch alert.Status {
		case AlertStatusOK:
			// 首次触发 → Pending；ForDuration<=0 表示立即触发，同次评估内直接转 Firing 并通知。
			alert.transition(eventTriggered)
			if time.Since(alert.StartedAt) >= rule.ForDuration {
				alert.transition(eventForDurationMet)
				alert.Message = result.Message
				m.notifyFiring(ctx, alert)
			}

		case AlertStatusPending:
			// 检查是否满足持续时间
			if time.Since(alert.StartedAt) >= rule.ForDuration {
				alert.transition(eventForDurationMet)
				alert.Message = result.Message
				// 推送告警事件
				m.notifyFiring(ctx, alert)
			}

		case AlertStatusFiring, AlertStatusAcknowledged:
			// 已经在告警中，检查是否需要重复通知
			if rule.RepeatInterval > 0 && time.Since(alert.LastNotified) >= rule.RepeatInterval {
				alert.transition(eventRepeat)
				alert.Message = result.Message
				m.notifyFiring(ctx, alert)
			}
		}
	} else {
		// 条件恢复
		switch alert.Status {
		case AlertStatusPending:
			// 持续时间内恢复了 → OK
			alert.transition(eventResolved)

		case AlertStatusFiring, AlertStatusAcknowledged:
			// 告警中恢复了 → Resolved
			alert.transition(eventResolved)
			alert.Message = fmt.Sprintf("%s 已恢复（当前值: %.2f%s）",
				rule.Name, result.Value, metricUnit(rule.Metric))
			m.notifyResolved(ctx, alert)
		}
	}
}

// ReportEvent 接收浏览器侧错误事件，匹配 frontend_event 规则并驱动状态机。
//
// 与 pull 型规则不同：事件只在到达时驱动一次状态转移；firing 期间的后续
// 事件仅刷新 LastEventAt（静默恢复计时），不重复推送，告警恢复由
// checkFrontendSilence 依据 RecoveryWindow 判定。
func (m *AlertManager) ReportEvent(ctx context.Context, evt *FrontendEvent) {
	if evt == nil {
		return
	}
	if evt.EventAt.IsZero() {
		evt.EventAt = time.Now()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rule := range m.rules {
		if !rule.Enabled || rule.Type != AlertRuleTypeFrontendEvent {
			continue
		}
		if rule.SandboxID != "" && rule.SandboxID != evt.SandboxID {
			continue
		}
		if !matchEventType(rule.EventTypes, evt.Type) {
			continue
		}

		alertID := generateAlertID(rule)
		alert, exists := m.alerts[alertID]
		if !exists {
			labels := make(map[string]string, len(rule.Labels)+2)
			for k, v := range rule.Labels {
				labels[k] = v
			}
			labels["source"] = AlertSourceFrontend
			if evt.Step != "" {
				labels["step"] = evt.Step
			}
			alert = &AlertInstance{
				ID:            alertID,
				RuleID:        rule.ID,
				RuleName:      rule.Name,
				AlertSeverity: rule.AlertSeverity,
				Status:        AlertStatusOK,
				SandboxID:     rule.SandboxID,
				Service:       rule.Service,
				Metric:        "frontend_event",
				Labels:        labels,
			}
			m.alerts[alertID] = alert
		}

		alert.LastEventAt = evt.EventAt
		if evt.Step != "" {
			if alert.Labels == nil {
				alert.Labels = map[string]string{}
			}
			alert.Labels["step"] = evt.Step
		}

		switch alert.Status {
		case AlertStatusOK:
			alert.transition(eventTriggered)
			// EventAt precedes the transition's wall-clock StartedAt for events
			// already in transit. A disabled hold period must not compare those clocks.
			if rule.ForDuration <= 0 || evt.EventAt.Sub(alert.StartedAt) >= rule.ForDuration {
				alert.transition(eventForDurationMet)
				alert.Message = formatFrontendEventMessage(rule, evt)
				m.notifyFiring(ctx, alert)
			}

		case AlertStatusPending:
			if rule.ForDuration <= 0 || evt.EventAt.Sub(alert.StartedAt) >= rule.ForDuration {
				alert.transition(eventForDurationMet)
				alert.Message = formatFrontendEventMessage(rule, evt)
				m.notifyFiring(ctx, alert)
			}

		case AlertStatusFiring, AlertStatusAcknowledged:
			// 抑制：已在告警中，仅刷新最近事件时间，不重复推送

		case AlertStatusResolved:
			// 恢复后再次出错：当作新告警，重置时间线
			alert.Status = AlertStatusOK
			alert.StartedAt = time.Time{}
			alert.FiredAt = time.Time{}
			alert.ResolvedAt = time.Time{}
			alert.LastNotified = time.Time{}
			alert.TriggerCount = 0
			alert.transition(eventTriggered)
			if rule.ForDuration <= 0 || evt.EventAt.Sub(alert.StartedAt) >= rule.ForDuration {
				alert.transition(eventForDurationMet)
				alert.Message = formatFrontendEventMessage(rule, evt)
				m.notifyFiring(ctx, alert)
			}
		}
	}
}

// matchEventType 判断事件类型是否被规则匹配（规则未声明类型时匹配全部）。
func matchEventType(eventTypes []string, evtType string) bool {
	if len(eventTypes) == 0 {
		return true
	}
	for _, t := range eventTypes {
		if t == evtType {
			return true
		}
	}
	return false
}

// checkFrontendSilence 扫描 frontend_event 告警，连续 RecoveryWindow 无新
// 事件则自动恢复。复用 30s 定时评估 ticker，不新增后台任务。
func (m *AlertManager) checkFrontendSilence(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, alert := range m.alerts {
		if alert.Status != AlertStatusFiring && alert.Status != AlertStatusAcknowledged && alert.Status != AlertStatusPending {
			continue
		}
		rule, ok := m.rules[alert.RuleID]
		if !ok || rule.Type != AlertRuleTypeFrontendEvent {
			continue
		}
		window := rule.RecoveryWindow
		if window <= 0 {
			window = defaultFrontendRecoveryWindow
		}
		last := alert.LastEventAt
		if last.IsZero() {
			last = alert.StartedAt
		}
		if now.Sub(last) < window {
			continue
		}

		if alert.Status == AlertStatusPending {
			// 尚未正式告警即恢复：静默回到 OK，不推送“恢复”事件，
			// 避免前端出现从未告警却先收到恢复通知的噪音。
			alert.transition(eventResolved)
			continue
		}

		alert.transition(eventResolved)
		alert.Message = fmt.Sprintf("%s 已恢复（连续 %d 秒无页面错误事件）",
			rule.Name, int(window.Seconds()))
		m.notifyResolved(ctx, alert)
	}
}

// notifyFiring 推送告警触发事件。
func (m *AlertManager) notifyFiring(ctx context.Context, alert *AlertInstance) {
	if m.publisher == nil {
		return
	}

	data := eventbus.AlertEventData{
		AlertID:   alert.ID,
		SandboxID: alert.SandboxID,
		RuleID:    alert.RuleID,
		RuleName:  alert.RuleName,
		Severity:  string(alert.AlertSeverity),
		Metric:    alert.Metric,
		Value:     alert.Value,
		Threshold: alert.Threshold,
		Message:   alert.Message,
		Timestamp: alert.FiredAt.UnixMilli(),
		Source:    alert.Labels["source"],
		Step:      alert.Labels["step"],
	}

	channel := eventbus.SandboxChannel(alert.SandboxID)
	m.publisher.PublishToChannel(ctx, channel, eventbus.EventAlert, data)

	if m.handler != nil {
		m.handler.OnAlertFiring(ctx, alert)
	}
}

// notifyResolved 推送告警恢复事件。
func (m *AlertManager) notifyResolved(ctx context.Context, alert *AlertInstance) {
	if m.publisher == nil {
		return
	}

	data := eventbus.AlertEventData{
		AlertID:   alert.ID,
		SandboxID: alert.SandboxID,
		RuleID:    alert.RuleID,
		RuleName:  alert.RuleName,
		Severity:  string(alert.AlertSeverity),
		Metric:    alert.Metric,
		Value:     alert.Value,
		Threshold: alert.Threshold,
		Message:   alert.Message,
		Timestamp: alert.ResolvedAt.UnixMilli(),
		Source:    alert.Labels["source"],
		Step:      alert.Labels["step"],
	}

	channel := eventbus.SandboxChannel(alert.SandboxID)
	m.publisher.PublishToChannel(ctx, channel, eventbus.EventAlert, data)

	if m.handler != nil {
		m.handler.OnAlertResolved(ctx, alert)
	}
}

// cleanupResolved 清理已恢复且超过保留期的告警。
func (m *AlertManager) cleanupResolved() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, alert := range m.alerts {
		if alert.Status == AlertStatusResolved {
			if now.Sub(alert.ResolvedAt) > m.retentionPeriod {
				delete(m.alerts, id)
			}
		}
	}
}

// generateAlertID 生成告警实例 ID。
//
// 基于规则ID + 沙箱ID + 服务名的哈希，确保同一规则同一目标只有一个告警实例。
func generateAlertID(rule *AlertRule) string {
	key := fmt.Sprintf("%s:%s:%s", rule.ID, rule.SandboxID, rule.Service)
	hash := sha1.Sum([]byte(key))
	return hex.EncodeToString(hash[:8]) // 取前 8 字节（16 个十六进制字符）
}
