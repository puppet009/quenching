// 告警评估器（T1-23）。
//
// 支持三种评估器：
//   - AlertThresholdEvaluator: 阈值型，指标值超过/低于阈值触发
//   - AlertTrendEvaluator: 趋势型，指标持续上升/下降触发
//   - AlertLogKeywordEvaluator: 日志关键词型，日志出现关键词触发
package service

import (
	"context"
	"strings"
	"time"
)

// ========== 数据源接口 ==========

// AlertMetricsSource 指标数据源。
type AlertMetricsSource interface {
	// GetCurrentValue 获取指标当前值。
	GetCurrentValue(ctx context.Context, sandboxID, service, metric string, aggregation string) (float64, error)

	// QueryRange 区间查询，返回数据点（用于趋势判断）。
	QueryRange(ctx context.Context, sandboxID, service, metric string, start, end time.Time, step int) ([]AlertDataPoint, error)
}

// AlertDataPoint 指标数据点。
type AlertDataPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

// AlertLogSource 日志数据源。
type AlertLogSource interface {
	// CountKeyword 统计指定时间范围内关键词出现的次数。
	CountKeyword(ctx context.Context, sandboxID, service, keyword, level string, start, end time.Time) (int, error)
}

// ========== 评估结果 ==========

// AlertEvalResult 评估结果。
type AlertEvalResult struct {
	Fired   bool    // 是否触发（条件满足）
	Value   float64 // 当前值（指标值或关键词计数）
	Message string  // 描述信息
}

// ========== 评估器接口 ==========

// AlertEvaluator 告警规则评估器。
type AlertEvaluator interface {
	// Evaluate 评估规则是否触发。
	Evaluate(ctx context.Context, rule *AlertRule) (AlertEvalResult, error)
}

// ========== 阈值型评估器 ==========

// AlertThresholdEvaluator 阈值型评估器。
type AlertThresholdEvaluator struct {
	metrics AlertMetricsSource
}

// NewAlertThresholdEvaluator 创建阈值型评估器。
func NewAlertThresholdEvaluator(metrics AlertMetricsSource) *AlertThresholdEvaluator {
	return &AlertThresholdEvaluator{metrics: metrics}
}

// Evaluate 评估阈值型规则。
func (e *AlertThresholdEvaluator) Evaluate(ctx context.Context, rule *AlertRule) (AlertEvalResult, error) {
	if rule.Type != AlertRuleTypeThreshold {
		return AlertEvalResult{}, nil
	}

	value, err := e.metrics.GetCurrentValue(ctx, rule.SandboxID, rule.Service, rule.Metric, "max")
	if err != nil {
		return AlertEvalResult{}, err
	}

	fired := evaluateThreshold(value, rule.AlertOperator, rule.Threshold)

	return AlertEvalResult{
		Fired:   fired,
		Value:   value,
		Message: formatThresholdMessage(rule, value),
	}, nil
}

// ========== 趋势型评估器 ==========

// AlertTrendEvaluator 趋势型评估器。
type AlertTrendEvaluator struct {
	metrics AlertMetricsSource
}

// NewAlertTrendEvaluator 创建趋势型评估器。
func NewAlertTrendEvaluator(metrics AlertMetricsSource) *AlertTrendEvaluator {
	return &AlertTrendEvaluator{metrics: metrics}
}

// Evaluate 评估趋势型规则。
//
// 思路：比较时间窗口内的前半段平均值和后半段平均值，
// 计算变化百分比，判断是否满足持续上升/下降的条件。
func (e *AlertTrendEvaluator) Evaluate(ctx context.Context, rule *AlertRule) (AlertEvalResult, error) {
	if rule.Type != AlertRuleTypeTrend {
		return AlertEvalResult{}, nil
	}

	duration := rule.TrendDuration
	if duration == 0 {
		duration = 5 * time.Minute
	}

	end := time.Now()
	start := end.Add(-duration)

	// 步长设为总时长的 1/20，至少 10 秒
	step := int(duration.Seconds() / 20)
	if step < 10 {
		step = 10
	}

	points, err := e.metrics.QueryRange(ctx, rule.SandboxID, rule.Service, rule.Metric, start, end, step)
	if err != nil {
		return AlertEvalResult{}, err
	}

	if len(points) < 4 {
		// 数据点太少，无法判断趋势
		return AlertEvalResult{Fired: false, Value: 0}, nil
	}

	// 分成前后两半
	mid := len(points) / 2
	firstHalf := points[:mid]
	secondHalf := points[mid:]

	firstAvg := averagePoints(firstHalf)
	secondAvg := averagePoints(secondHalf)

	if firstAvg == 0 {
		return AlertEvalResult{Fired: false, Value: 0}, nil
	}

	// 计算变化百分比
	changePct := ((secondAvg - firstAvg) / firstAvg) * 100

	var fired bool
	switch rule.AlertTrendDirection {
	case AlertTrendUp:
		fired = changePct >= rule.TrendChangePct
	case AlertTrendDown:
		fired = changePct <= -rule.TrendChangePct
	}

	return AlertEvalResult{
		Fired:   fired,
		Value:   changePct,
		Message: formatTrendMessage(rule, changePct),
	}, nil
}

// averagePoints 计算数据点的平均值。
func averagePoints(points []AlertDataPoint) float64 {
	if len(points) == 0 {
		return 0
	}

	var sum float64
	for _, p := range points {
		sum += p.Value
	}
	return sum / float64(len(points))
}

// ========== 日志关键词型评估器 ==========

// AlertLogKeywordEvaluator 日志关键词型评估器。
type AlertLogKeywordEvaluator struct {
	logs AlertLogSource
	// 评估时间窗口（默认 1 分钟）
	Window time.Duration
	// 触发阈值（关键词出现次数，默认 1）
	Threshold int
}

// NewAlertLogKeywordEvaluator 创建日志关键词型评估器。
func NewAlertLogKeywordEvaluator(logs AlertLogSource) *AlertLogKeywordEvaluator {
	return &AlertLogKeywordEvaluator{
		logs:      logs,
		Window:    1 * time.Minute,
		Threshold: 1,
	}
}

// Evaluate 评估日志关键词规则。
func (e *AlertLogKeywordEvaluator) Evaluate(ctx context.Context, rule *AlertRule) (AlertEvalResult, error) {
	if rule.Type != AlertRuleTypeLogKeyword {
		return AlertEvalResult{}, nil
	}

	window := e.Window
	if window == 0 {
		window = 1 * time.Minute
	}

	threshold := e.Threshold
	if threshold == 0 {
		threshold = 1
	}

	end := time.Now()
	start := end.Add(-window)

	count, err := e.logs.CountKeyword(ctx, rule.SandboxID, rule.Service, rule.LogKeyword, rule.LogLevel, start, end)
	if err != nil {
		return AlertEvalResult{}, err
	}

	fired := count >= threshold

	return AlertEvalResult{
		Fired:   fired,
		Value:   float64(count),
		Message: formatLogKeywordMessage(rule),
	}, nil
}

// ========== 复合评估器 ==========

// AlertCompositeEvaluator 复合评估器。
//
// 根据规则类型自动选择合适的评估器。
type AlertCompositeEvaluator struct {
	threshold *AlertThresholdEvaluator
	trend     *AlertTrendEvaluator
	logKW     *AlertLogKeywordEvaluator
}

// NewAlertCompositeEvaluator 创建复合评估器。
func NewAlertCompositeEvaluator(metrics AlertMetricsSource, logs AlertLogSource) *AlertCompositeEvaluator {
	return &AlertCompositeEvaluator{
		threshold: NewAlertThresholdEvaluator(metrics),
		trend:     NewAlertTrendEvaluator(metrics),
		logKW:     NewAlertLogKeywordEvaluator(logs),
	}
}

// Evaluate 根据规则类型选择评估器。
func (e *AlertCompositeEvaluator) Evaluate(ctx context.Context, rule *AlertRule) (AlertEvalResult, error) {
	switch rule.Type {
	case AlertRuleTypeThreshold:
		return e.threshold.Evaluate(ctx, rule)
	case AlertRuleTypeTrend:
		return e.trend.Evaluate(ctx, rule)
	case AlertRuleTypeLogKeyword:
		return e.logKW.Evaluate(ctx, rule)
	default:
		return AlertEvalResult{Fired: false}, nil
	}
}

// ========== 辅助函数 ==========

// containsKeyword 检查字符串是否包含关键词（大小写不敏感）。
func containsKeyword(text, keyword string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}
