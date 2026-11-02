package service

// Weights 评分权重与阈值（集中配置，便于运营调参）。
type Weights struct {
	// 技术分各部分权重（合计 1.0）。
	TechResolve  float64 // 是否修复（占比最大）
	TechDuration float64 // 耗时表现
	TechCost     float64 // 操作代价（低代价得分）
	TechHint     float64 // 提示独立性（少用提示得分）

	// 协作三维子分权重（合计 1.0）。
	CollabQuestion float64
	CollabInfo     float64
	CollabJudgment float64

	// 综合分中两维的权重（合计 1.0）。
	OverallTechWeight   float64
	OverallCollabWeight float64

	// ResolveBaseScore 修复/未修复的基础分。
	ResolvedBase   float64
	UnresolvedBase float64
}

// DefaultWeights 默认权重。
func DefaultWeights() Weights {
	return Weights{
		TechResolve:  0.5,
		TechDuration: 0.2,
		TechCost:     0.2,
		TechHint:     0.1,

		CollabQuestion: 0.3,
		CollabInfo:     0.3,
		CollabJudgment: 0.4, // 判断力（批判性使用AI）权重最高，呼应产品定位

		OverallTechWeight:   0.6,
		OverallCollabWeight: 0.4,

		ResolvedBase:   100,
		UnresolvedBase: 40,
	}
}

// 提示独立性：每用一次提示的扣分上限配置。
const hintPenaltyPerUse = 8.0
const maxHintPenalty = 40.0

// clamp 限定 0-100。
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
