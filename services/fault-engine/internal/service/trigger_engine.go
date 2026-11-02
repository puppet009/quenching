// trigger_engine.go 故障调度：根据剧本触发条件和运行时上下文，判定何时注入。
// 纯逻辑、不直接操作 K8s/blade——执行由上层调 Injector，本文件只回答"现在该不该触发"。
package service

// TriggerRuntimeContext 每轮调度时的运行时输入。
type TriggerRuntimeContext struct {
	// ElapsedSeconds 实训已进行时长（从会话 running 起算）。
	ElapsedSeconds int
	// Metrics 当前指标采样（metric 触发用）。
	Metrics []JudgeMetricSample
}

// TriggerState 触发状态机。一个剧本对应一个实例，保证"只触发一次"。
type TriggerState struct {
	scenario *Scenario
	fired    bool
	missed   bool // 已错过时间窗口，永远不会再触发
}

// NewTriggerState 构造触发状态机。
func NewTriggerState(s *Scenario) *TriggerState {
	return &TriggerState{scenario: s}
}

// TriggerDecision 判定结论。
type TriggerDecision int

const (
	TriggerDecisionWait    TriggerDecision = iota // 还不到时候，继续等
	TriggerDecisionFire                           // 立即注入
	TriggerDecisionExpired                        // 已错过窗口，不再触发（清理状态）
)

// Evaluate 纯函数式推进：给当前上下文，返回该做什么。
// 已触发后永远返回 Wait（幂等，避免重复注入）。
func (s *TriggerState) Evaluate(rc TriggerRuntimeContext) TriggerDecision {
	if s.fired {
		return TriggerDecisionWait
	}
	if s.missed {
		return TriggerDecisionExpired
	}

	tr := s.scenario.Trigger
	switch tr.EffectiveType() {
	case TriggerDelay:
		if rc.ElapsedSeconds >= tr.DelaySeconds {
			return TriggerDecisionFire
		}
		return TriggerDecisionWait

	case TriggerMetric:
		// 无时间窗口约束：指标条件满足即触发。
		if metricMet(tr.Metric, rc.Metrics) {
			return TriggerDecisionFire
		}
		return TriggerDecisionWait

	case TriggerWindow:
		w := tr.Window
		// 已过窗口结束时间：无论是否带指标条件，都算错过，放弃触发。
		if rc.ElapsedSeconds >= w.EndSeconds {
			s.missed = true
			return TriggerDecisionExpired
		}
		// 在窗口内：有指标条件则需满足，无指标条件则进入窗口即触发。
		if rc.ElapsedSeconds >= w.StartSeconds {
			if metricMetOrNone(tr.Metric, rc.Metrics) {
				return TriggerDecisionFire
			}
		}
		return TriggerDecisionWait
	}
	return TriggerDecisionWait
}

// MarkFired 上层成功注入后调用，锁定状态（只触发一次）。
func (s *TriggerState) MarkFired() { s.fired = true }

// Fired 是否已触发。
func (s *TriggerState) Fired() bool { return s.fired }

// metricMetOrNone 有条件则判定，无条件（纯时间窗）视为满足。
func metricMetOrNone(c *MetricCondition, samples []JudgeMetricSample) bool {
	if c == nil {
		return true
	}
	return metricMet(c, samples)
}

// metricMet 判定指标条件是否满足。
func metricMet(c *MetricCondition, samples []JudgeMetricSample) bool {
	if c == nil {
		return false
	}
	for _, sm := range samples {
		if sm.Name != c.Metric {
			continue
		}
		if c.Service != "" && sm.Service != c.Service {
			continue
		}
		switch c.Op {
		case "gt":
			if sm.Value > c.Value {
				return true
			}
		case "gte":
			if sm.Value >= c.Value {
				return true
			}
		case "lt":
			if sm.Value < c.Value {
				return true
			}
		case "lte":
			if sm.Value <= c.Value {
				return true
			}
		}
	}
	return false
}
