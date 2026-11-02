package service

import "time"

// JudgeEvaluator 单场故障的修复判定器（每个训练会话/故障一个实例）。
// 它是有状态的：记录"从什么时候起所有指标持续正常"。
// 一旦中途有任一指标再次异常，连续计时清零——这就排除了'表面恢复'。
type JudgeEvaluator struct {
	thresholds     []JudgeThreshold
	stableRequired time.Duration

	// healthySince 所有指标首次恢复正常的时间；零值表示当前并非全部正常。
	healthySince time.Time
}

// NewJudgeEvaluator 构造判定器。stableRequired 为稳定窗口（如 30s）。
func NewJudgeEvaluator(thresholds []JudgeThreshold, stableRequired time.Duration) *JudgeEvaluator {
	if stableRequired <= 0 {
		stableRequired = 30 * time.Second
	}
	return &JudgeEvaluator{thresholds: thresholds, stableRequired: stableRequired}
}

// Evaluate 输入当前一轮采样值和采样时间，返回判定结果。
// 每轮由上层轮询调用（如每 5s）。
func (e *JudgeEvaluator) Evaluate(samples []JudgeMetricSample, now time.Time) JudgeResult {
	abnormal := e.checkAbnormal(samples)

	if len(abnormal) > 0 {
		// 仍有异常（或再次恶化）：清零连续达标计时。
		e.healthySince = time.Time{}
		return JudgeResult{
			Verdict:        JudgeVerdictNotResolved,
			Abnormal:       abnormal,
			StableRequired: e.stableRequired,
		}
	}

	// 所有指标正常。
	if e.healthySince.IsZero() {
		e.healthySince = now // 开始持续达标
	}
	stableFor := now.Sub(e.healthySince)

	res := JudgeResult{
		StableFor:      stableFor,
		StableRequired: e.stableRequired,
	}
	if stableFor >= e.stableRequired {
		res.Verdict = JudgeVerdictResolved
	} else {
		res.Verdict = JudgeVerdictNotResolved // 达标但还没稳定够久，暂不判定解决
	}
	return res
}

// checkAbnormal 找出当前仍异常的指标。
// 指标缺失也视为异常（防止服务挂了采不到数据却误判恢复）——但允许通过
// optional 标记豁免；MVP 默认缺失即异常，更安全。
func (e *JudgeEvaluator) checkAbnormal(samples []JudgeMetricSample) []string {
	byKey := make(map[string]float64, len(samples))
	for _, s := range samples {
		byKey[key(s.Service, s.Name)] = s.Value
	}

	var abnormal []string
	for _, th := range e.thresholds {
		v, ok := byKey[key(th.Service, th.Name)]
		if !ok {
			abnormal = append(abnormal, display(th)+"(无数据)")
			continue
		}
		if !passes(v, th) {
			abnormal = append(abnormal, display(th))
		}
	}
	return abnormal
}

// passes 单个值是否满足正常条件。
func passes(v float64, th JudgeThreshold) bool {
	switch th.Op {
	case JudgeOpLTE:
		return v <= th.Value
	case JudgeOpGTE:
		return v >= th.Value
	default:
		return false
	}
}

func key(service, name string) string { return service + "|" + name }

func display(th JudgeThreshold) string {
	if th.Service != "" {
		return th.Service + "/" + th.Name
	}
	return th.Name
}
