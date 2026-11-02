// 告警规则引擎（T1-23，原 internal/alert 包，g6 批次3 并入 service）。
//
// 支持阈值型、趋势型、日志关键词型告警规则，
// 提供告警状态机、去重、抑制和事件推送能力。
package service

import (
	"fmt"
	"time"
)

// ========== 告警级别 ==========

// AlertSeverity 告警级别。
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"     // 提示
	AlertSeverityWarning  AlertSeverity = "warning"  // 警告
	AlertSeverityCritical AlertSeverity = "critical" // 严重
)

// ========== 规则类型 ==========

// AlertRuleType 告警规则类型。
type AlertRuleType string

const (
	// AlertRuleTypeThreshold 阈值型：指标值超过/低于阈值时触发
	AlertRuleTypeThreshold AlertRuleType = "threshold"

	// AlertRuleTypeTrend 趋势型：指标在一段时间内持续上升/下降触发
	AlertRuleTypeTrend AlertRuleType = "trend"

	// AlertRuleTypeLogKeyword 日志关键词型：日志中出现指定关键词触发
	AlertRuleTypeLogKeyword AlertRuleType = "log_keyword"

	// AlertRuleTypeFrontendEvent 前端事件型：行为引擎上报浏览器侧错误触发
	// （步骤失败/页面异常/控制台报错/请求失败）。事件驱动，不做 pull 评估，
	// 连续 RecoveryWindow 无事件即自动恢复。
	AlertRuleTypeFrontendEvent AlertRuleType = "frontend_event"
)

// ========== 前端行为信号类型（与 sandbox-service runner 上报同名字段） ==========

const (
	FrontendSignalStepFailure   = "step_failure"   // 页面操作步骤失败（点击/下单/查询等）
	FrontendSignalPageError     = "page_error"     // 未捕获脚本异常（可能白屏）
	FrontendSignalConsoleError  = "console_error"  // 控制台报错
	FrontendSignalHTTPError     = "http_error"     // 接口响应状态码 >= 400
	FrontendSignalRequestFailed = "request_failed" // 请求发出即失败（网络/DNS/跨域）
)

// 告警来源。
const (
	AlertSourceBackend  = "backend"  // 后端指标/日志
	AlertSourceFrontend = "frontend" // 浏览器侧观测
)

// ========== 比较操作符 ==========

// AlertOperator 比较操作符。
type AlertOperator string

const (
	AlertOpGreaterThan      AlertOperator = ">"  // 大于
	AlertOpGreaterThanEqual AlertOperator = ">=" // 大于等于
	AlertOpLessThan         AlertOperator = "<"  // 小于
	AlertOpLessThanEqual    AlertOperator = "<=" // 小于等于
	AlertOpEqual            AlertOperator = "==" // 等于
	AlertOpNotEqual         AlertOperator = "!=" // 不等于
)

// ========== 趋势方向 ==========

// AlertTrendDirection 趋势方向。
type AlertTrendDirection string

const (
	AlertTrendUp   AlertTrendDirection = "up"   // 持续上升
	AlertTrendDown AlertTrendDirection = "down" // 持续下降
)

// ========== 告警状态 ==========

// AlertStatus 告警状态。
type AlertStatus string

const (
	// AlertStatusOK 正常（未触发）
	AlertStatusOK AlertStatus = "ok"

	// AlertStatusPending 待确认（首次触发，等待持续时间验证）
	AlertStatusPending AlertStatus = "pending"

	// AlertStatusFiring 告警中（已触发且满足持续时间）
	AlertStatusFiring AlertStatus = "firing"

	// AlertStatusAcknowledged 已确认（有人确认了告警）
	AlertStatusAcknowledged AlertStatus = "acknowledged"

	// AlertStatusResolved 已恢复（指标回到正常范围）
	AlertStatusResolved AlertStatus = "resolved"
)

// ========== 前端事件输入 ==========

// FrontendEvent 行为引擎上报的浏览器侧错误事件（字段与 sandbox-service
// BehaviorSignal 同名同义，由内部 HTTP 端点转换而来）。
type FrontendEvent struct {
	SandboxID string    // 沙箱ID
	Type      string    // 信号类型（五类之一）
	Severity  string    // 信号自带级别（可选）
	Step      string    // 失败的行为步骤名
	Action    string    // 步骤动作（click/fill 等）
	Message   string    // 错误描述
	URL       string    // 相关页面/请求地址
	Status    int       // HTTP 状态码（http_error 时）
	EventAt   time.Time // 事件发生时间（零值取当前时间）
}

// ========== 告警规则定义 ==========

// AlertRule 告警规则。
type AlertRule struct {
	ID            string        `json:"id"`          // 规则ID
	Name          string        `json:"name"`        // 规则名称
	Description   string        `json:"description"` // 规则描述
	Type          AlertRuleType `json:"type"`        // 规则类型
	AlertSeverity AlertSeverity `json:"severity"`    // 告警级别
	Enabled       bool          `json:"enabled"`     // 是否启用

	// 目标
	SandboxID string `json:"sandbox_id"` // 沙箱ID
	Service   string `json:"service"`    // 服务名（空=所有服务）

	// 阈值型参数
	Metric        string        `json:"metric,omitempty"`    // 指标名（cpu_usage/memory_usage/qps/error_rate/p99_latency）
	AlertOperator AlertOperator `json:"operator,omitempty"`  // 比较操作符
	Threshold     float64       `json:"threshold,omitempty"` // 阈值

	// 趋势型参数
	AlertTrendDirection AlertTrendDirection `json:"trend_direction,omitempty"`  // 趋势方向
	TrendChangePct      float64             `json:"trend_change_pct,omitempty"` // 变化百分比阈值
	TrendDuration       time.Duration       `json:"trend_duration,omitempty"`   // 持续时间

	// 日志关键词型参数
	LogKeyword string `json:"log_keyword,omitempty"` // 关键词
	LogLevel   string `json:"log_level,omitempty"`   // 日志级别过滤（可选）

	// 前端事件型参数
	EventTypes []string `json:"event_types,omitempty"` // 匹配的前端信号类型（空=全部五类）

	// 通用参数
	ForDuration    time.Duration `json:"for_duration"`    // 持续多久才触发（防抖动）
	RepeatInterval time.Duration `json:"repeat_interval"` // 重复告警间隔（0=不重复）

	// RecoveryWindow 前端事件型自动恢复窗口：连续该时长无新事件即 resolved。
	// 0 时由管理器按默认 60 秒处理。
	RecoveryWindow time.Duration `json:"recovery_window,omitempty"`

	// 标签（附加信息，随告警事件一起推送）
	Labels map[string]string `json:"labels,omitempty"`
}

// ========== 告警实例 ==========

// AlertInstance 告警实例（一条具体的告警）。
type AlertInstance struct {
	ID            string            `json:"id"`               // 告警实例ID（规则ID + 标签哈希）
	RuleID        string            `json:"rule_id"`          // 规则ID
	RuleName      string            `json:"rule_name"`        // 规则名称
	AlertSeverity AlertSeverity     `json:"severity"`         // 级别
	Status        AlertStatus       `json:"status"`           // 状态
	SandboxID     string            `json:"sandbox_id"`       // 沙箱ID
	Service       string            `json:"service"`          // 服务名
	Metric        string            `json:"metric"`           // 关联指标
	Value         float64           `json:"value"`            // 当前值
	Threshold     float64           `json:"threshold"`        // 阈值
	Message       string            `json:"message"`          // 告警消息
	Labels        map[string]string `json:"labels,omitempty"` // 附加标签

	// 时间
	StartedAt    time.Time `json:"started_at"`    // 首次触发时间
	FiredAt      time.Time `json:"fired_at"`      // 正式告警时间（满足持续时间后）
	ResolvedAt   time.Time `json:"resolved_at"`   // 恢复时间
	LastNotified time.Time `json:"last_notified"` // 上次通知时间
	LastEventAt  time.Time `json:"last_event_at"` // 前端事件型：最近一次事件时间

	// 统计
	TriggerCount int `json:"trigger_count"` // 触发次数
}

// ========== 状态机 ==========

// stateTransition 状态转移。
type stateTransition struct {
	From   AlertStatus
	Event  stateEvent
	To     AlertStatus
	Action func(*AlertInstance)
}

type stateEvent int

const (
	eventTriggered      stateEvent = iota // 条件满足
	eventResolved                         // 条件恢复
	eventForDurationMet                   // 持续时间达到
	eventAcknowledged                     // 被确认
	eventRepeat                           // 重复告警
)

// transitions 状态转移表。
var transitions = []stateTransition{
	// OK → Pending：首次触发
	{From: AlertStatusOK, Event: eventTriggered, To: AlertStatusPending, Action: func(a *AlertInstance) {
		a.StartedAt = time.Now()
		a.TriggerCount++
	}},

	// Pending → Firing：持续时间满足
	{From: AlertStatusPending, Event: eventForDurationMet, To: AlertStatusFiring, Action: func(a *AlertInstance) {
		a.FiredAt = time.Now()
		a.LastNotified = time.Now()
	}},

	// Pending → OK：在持续时间内恢复了
	{From: AlertStatusPending, Event: eventResolved, To: AlertStatusOK, Action: func(a *AlertInstance) {
		a.StartedAt = time.Time{}
	}},

	// Firing → Firing (重复通知)
	{From: AlertStatusFiring, Event: eventRepeat, To: AlertStatusFiring, Action: func(a *AlertInstance) {
		a.LastNotified = time.Now()
	}},

	// Firing → Acknowledged：有人确认
	{From: AlertStatusFiring, Event: eventAcknowledged, To: AlertStatusAcknowledged, Action: nil},

	// Acknowledged → Acknowledged (重复通知)
	{From: AlertStatusAcknowledged, Event: eventRepeat, To: AlertStatusAcknowledged, Action: func(a *AlertInstance) {
		a.LastNotified = time.Now()
	}},

	// Firing → Resolved：恢复
	{From: AlertStatusFiring, Event: eventResolved, To: AlertStatusResolved, Action: func(a *AlertInstance) {
		a.ResolvedAt = time.Now()
	}},

	// Acknowledged → Resolved：恢复
	{From: AlertStatusAcknowledged, Event: eventResolved, To: AlertStatusResolved, Action: func(a *AlertInstance) {
		a.ResolvedAt = time.Now()
	}},

	// Resolved → OK：清除（下次触发是新告警）
	// 注意：Resolved 状态保留一段时间后由管理器清理，这里不直接转 OK
}

// transition 执行状态转移。
//
// 返回 true 表示状态发生了变化。
func (a *AlertInstance) transition(evt stateEvent) bool {
	for _, t := range transitions {
		if t.From == a.Status && t.Event == evt {
			oldStatus := a.Status
			a.Status = t.To
			if t.Action != nil {
				t.Action(a)
			}
			return oldStatus != a.Status
		}
	}
	return false
}

// shouldNotify 判断是否应该发送通知。
//
// 规则：
// - 新触发（进入 Firing 状态）→ 通知
// - 重复告警间隔到了 → 通知
// - 恢复（进入 Resolved 状态）→ 通知
func (a *AlertInstance) shouldNotify(repeatInterval time.Duration) bool {
	switch a.Status {
	case AlertStatusFiring:
		// 刚触发或重复间隔到了
		if time.Since(a.LastNotified) >= repeatInterval {
			return true
		}
	case AlertStatusResolved:
		// 刚恢复，还没发过恢复通知
		return !a.ResolvedAt.IsZero() && a.ResolvedAt.After(a.LastNotified)
	}
	return false
}

// ========== 工具函数 ==========

// evaluateThreshold 计算阈值比较。
func evaluateThreshold(value float64, op AlertOperator, threshold float64) bool {
	switch op {
	case AlertOpGreaterThan:
		return value > threshold
	case AlertOpGreaterThanEqual:
		return value >= threshold
	case AlertOpLessThan:
		return value < threshold
	case AlertOpLessThanEqual:
		return value <= threshold
	case AlertOpEqual:
		return value == threshold
	case AlertOpNotEqual:
		return value != threshold
	default:
		return false
	}
}

// formatMessage 格式化告警消息。
func formatMessage(rule *AlertRule, value float64) string {
	switch rule.Type {
	case AlertRuleTypeThreshold:
		return formatThresholdMessage(rule, value)
	case AlertRuleTypeTrend:
		return formatTrendMessage(rule, value)
	case AlertRuleTypeLogKeyword:
		return formatLogKeywordMessage(rule)
	case AlertRuleTypeFrontendEvent:
		return rule.Name
	default:
		return rule.Name
	}
}

func formatThresholdMessage(rule *AlertRule, value float64) string {
	service := rule.Service
	if service == "" {
		service = "全部服务"
	}
	return sprintf("%s (%s) 当前值 %.2f%s %s 阈值 %.2f%s",
		rule.Name, service, value,
		metricUnit(rule.Metric),
		operatorText(rule.AlertOperator),
		rule.Threshold,
		metricUnit(rule.Metric))
}

func formatTrendMessage(rule *AlertRule, changePct float64) string {
	direction := "上升"
	if rule.AlertTrendDirection == AlertTrendDown {
		direction = "下降"
	}
	return sprintf("%s 在 %s 内持续%s %.1f%%，超过阈值 %.1f%%",
		rule.Name, rule.TrendDuration, direction, changePct, rule.TrendChangePct)
}

func formatLogKeywordMessage(rule *AlertRule) string {
	level := rule.LogLevel
	if level == "" {
		level = "所有级别"
	}
	return sprintf("%s: 日志中出现关键词 '%s'（级别: %s）",
		rule.Name, rule.LogKeyword, level)
}

// formatFrontendEventMessage 按信号类型格式化前端事件告警消息。
func formatFrontendEventMessage(rule *AlertRule, evt *FrontendEvent) string {
	where := evt.Step
	if where == "" {
		where = evt.Action
	}
	if where == "" {
		where = "页面操作"
	}
	switch evt.Type {
	case FrontendSignalStepFailure:
		return sprintf("%s：行为步骤「%s」失败（%s）", rule.Name, where, truncateMsg(evt.Message, 60))
	case FrontendSignalPageError:
		return sprintf("%s：页面脚本异常，可能导致白屏或功能不可用（%s）", rule.Name, truncateMsg(evt.Message, 60))
	case FrontendSignalConsoleError:
		return sprintf("%s：浏览器控制台报错（%s）", rule.Name, truncateMsg(evt.Message, 60))
	case FrontendSignalHTTPError:
		return sprintf("%s：页面接口返回异常状态码 %d（%s）", rule.Name, evt.Status, truncateMsg(evt.URL, 60))
	case FrontendSignalRequestFailed:
		return sprintf("%s：页面请求发出后失败（网络/DNS/跨域，%s）", rule.Name, truncateMsg(evt.URL, 60))
	default:
		return rule.Name
	}
}

// truncateMsg 截断过长的描述文本。
func truncateMsg(s string, n int) string {
	if s == "" {
		return "无详细信息"
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// metricUnit 获取指标单位。
func metricUnit(metric string) string {
	switch metric {
	case "cpu_usage", "memory_usage", "error_rate":
		return "%"
	case "qps":
		return " 次/秒"
	case "p99_latency":
		return " 秒"
	default:
		return ""
	}
}

// operatorText 操作符文本。
func operatorText(op AlertOperator) string {
	switch op {
	case AlertOpGreaterThan:
		return "大于"
	case AlertOpGreaterThanEqual:
		return "大于等于"
	case AlertOpLessThan:
		return "小于"
	case AlertOpLessThanEqual:
		return "小于等于"
	case AlertOpEqual:
		return "等于"
	case AlertOpNotEqual:
		return "不等于"
	default:
		return string(op)
	}
}

// sprintf 格式化字符串。
func sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}
