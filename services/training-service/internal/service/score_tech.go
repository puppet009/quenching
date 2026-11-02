package service

import "github.com/quenching/training-service/internal/model"

// TechInput 技术分计算输入（来自会话与操作时间线统计）。
type TechInput struct {
	// FaultResolved 是否成功修复故障（未修复/超时/放弃为 false）。
	FaultResolved bool
	// DurationSec 实际排查耗时（从实训开始到修复/结束）。
	DurationSec int
	// ExpectedSec 该故障难度档位的参考时长（用于耗时得分，0 则不考评耗时）。
	ExpectedSec int
	// CostPenalty 操作代价扣分（危险操作/无效操作累计的惩罚分，0-100 量纲）。
	CostPenalty float64
	// HintCount 使用的提示次数（渐进式提示，用得越多越依赖）。
	HintCount int
}

// TechScore 计算排障技术分（0-100）。
//
// 构成（按 Weights）：
//   - resolve：是否修复（ResolvedBase/UnresolvedBase）
//   - duration：耗时表现（相对参考时长，越快越高；超时衰减）
//   - cost：操作代价（100 - 累计代价惩罚）
//   - hint：提示独立性（少用提示得分高）
func TechScore(in TechInput, w Weights) float64 {
	resolve := w.ResolvedBase
	if !in.FaultResolved {
		resolve = w.UnresolvedBase
	}

	duration := DurationScore(in)
	cost := clamp(100 - in.CostPenalty)
	hint := hintScore(in.HintCount)

	total := resolve*w.TechResolve +
		duration*w.TechDuration +
		cost*w.TechCost +
		hint*w.TechHint
	return clamp(total)
}

// TechScoreFromActions 从操作记录直接计算技术分（便捷函数）。
//
// 内部会先调用 CalcCostPenalty 计算操作代价惩罚，
// 再调用 TechScore 计算最终技术分。
// 适用于已经有完整操作记录的场景（如训练结束后评分）。
func TechScoreFromActions(in TechInput, actions []model.OperationAction, w Weights) (float64, CostResult) {
	costResult := CalcCostPenalty(actions)
	in.CostPenalty = costResult.Penalty
	score := TechScore(in, w)
	return score, costResult
}

// DurationScore 耗时得分：
//   - 未提供参考时长（ExpectedSec=0）时不给耗时分（返回中性，不参与区分）。
//   - 实际<=参考时长给满分；超过则按超时比例线性衰减，2倍参考时长降到0。
func DurationScore(in TechInput) float64 {
	if in.ExpectedSec <= 0 {
		return 60 // 无基准时给中性分，避免该维度极端影响
	}
	if in.DurationSec <= 0 {
		return 100
	}
	ratio := float64(in.DurationSec) / float64(in.ExpectedSec)
	if ratio <= 1 {
		return 100
	}
	if ratio >= 2 {
		return 0
	}
	// 1x~2x 线性衰减 100->0
	return clamp((2 - ratio) * 100)
}

// hintScore 提示独立性得分：每用一次提示扣分，封顶 maxHintPenalty。
func hintScore(hintCount int) float64 {
	if hintCount <= 0 {
		return 100
	}
	penalty := float64(hintCount) * hintPenaltyPerUse
	if penalty > maxHintPenalty {
		penalty = maxHintPenalty
	}
	return clamp(100 - penalty)
}
