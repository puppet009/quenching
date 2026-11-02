package service

import "fmt"

// TriggerType 触发方式。
type TriggerType string

const (
	// TriggerDelay 实训开始后延迟固定时间触发（最简单稳定，MVP 主用）。
	TriggerDelay TriggerType = "delay"
	// TriggerMetric 满足指标条件时触发（如流量达到某水位再注入）。
	TriggerMetric TriggerType = "metric"
	// TriggerWindow 在时间窗口内触发（如实训第 N 秒到第 M 秒之间，条件满足才触发）。
	TriggerWindow TriggerType = "window"
)

// Trigger 故障触发条件。
type Trigger struct {
	// Type 触发方式，默认 delay。
	Type TriggerType `json:"type"`
	// DelaySeconds 延迟触发：实训开始后多少秒注入。
	DelaySeconds int `json:"delay_seconds,omitempty"`
	// Window 时间窗口：[StartSeconds, EndSeconds)。
	Window *TimeWindow `json:"window,omitempty"`
	// Metric 指标条件（metric/window 方式用）。
	Metric *MetricCondition `json:"metric,omitempty"`
}

// TimeWindow 相对实训开始的秒级时间窗口。
type TimeWindow struct {
	StartSeconds int `json:"start_seconds"`
	EndSeconds   int `json:"end_seconds"`
}

// MetricCondition 指标触发条件。
type MetricCondition struct {
	Metric  string `json:"metric"`
	Service string `json:"service,omitempty"`
	// Op gt/gte/lt/lte
	Op    string  `json:"op"`
	Value float64 `json:"value"`
}

// Validate 触发条件合法性。
func (t Trigger) Validate(sceneCode string) error {
	switch t.Type {
	case "", TriggerDelay:
		if t.DelaySeconds < 0 {
			return fmt.Errorf("剧本 %s 延迟触发 delay_seconds 不能为负", sceneCode)
		}
	case TriggerMetric:
		if t.Metric == nil {
			return fmt.Errorf("剧本 %s metric 触发缺少条件", sceneCode)
		}
		if err := t.Metric.validate(); err != nil {
			return err
		}
	case TriggerWindow:
		if t.Window == nil {
			return fmt.Errorf("剧本 %s window 触发缺少窗口", sceneCode)
		}
		if t.Window.StartSeconds < 0 || t.Window.EndSeconds <= t.Window.StartSeconds {
			return fmt.Errorf("剧本 %s 时间窗口非法", sceneCode)
		}
		if t.Metric != nil {
			if err := t.Metric.validate(); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("剧本 %s 不支持的触发类型: %s", sceneCode, t.Type)
	}
	return nil
}

func (m *MetricCondition) validate() error {
	switch m.Op {
	case "gt", "gte", "lt", "lte":
	default:
		return fmt.Errorf("指标条件 op 非法: %s", m.Op)
	}
	if m.Metric == "" {
		return fmt.Errorf("指标条件缺少 metric")
	}
	return nil
}

// EffectiveType 空值归一化为默认 delay。
func (t Trigger) EffectiveType() TriggerType {
	if t.Type == "" {
		return TriggerDelay
	}
	return t.Type
}
