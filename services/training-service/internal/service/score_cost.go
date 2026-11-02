package service

import "github.com/quenching/training-service/internal/model"

// 操作代价计分（T2-17）。
//
// 核心思想：
//   - 操作越精准、越少走弯路，代价越低，得分越高
//   - 信息收集类操作代价低（鼓励先观察再动手）
//   - 变更类/危险操作代价高（每一次操作都要谨慎）
//   - 盲目操作、重复操作有额外惩罚
//
// 代价分类（从低到高）：
//   1. 信息收集类（查看指标/日志/配置/告警）→ 低代价
//   2. AI 提问 → 中低代价
//   3. 诊断执行（执行命令） → 中代价
//   4. 资源变更（修改配置/扩容） → 较高代价
//   5. 危险操作（重启服务） → 高代价

// CostResult 操作代价计算结果。
type CostResult struct {
	// Penalty 总惩罚分（0-100），操作代价得分 = 100 - Penalty。
	Penalty float64
	// BaseCost 基础代价（各操作类型代价之和）。
	BaseCost float64
	// RepeatPenalty 重复操作惩罚分。
	RepeatPenalty float64
	// BlindPenalty 盲目操作惩罚分（未充分观察就动手）。
	BlindPenalty float64
	// Breakdown 各类型操作统计。
	Breakdown map[model.OperationType]OperationCostStat
}

// OperationCostStat 单种操作的代价统计。
type OperationCostStat struct {
	Count     int     // 操作次数
	BaseCost  float64 // 基础代价
	ExtraCost float64 // 额外惩罚（重复/盲目等）
	TotalCost float64 // 总代价
}

// opCostConfig 各操作类型的基础代价配置。
var opCostConfig = map[model.OperationType]struct {
	baseCost float64 // 单次基础代价（分）
	category string  // 分类：info / diag / change / danger
}{
	model.OpViewMetric:     {baseCost: 1.0, category: "info"},
	model.OpViewLog:        {baseCost: 1.0, category: "info"},
	model.OpViewConfig:     {baseCost: 1.0, category: "info"},
	model.OpViewAlert:      {baseCost: 1.0, category: "info"},
	model.OpAIQuestion:     {baseCost: 2.0, category: "info"},
	model.OpExecCommand:    {baseCost: 3.0, category: "diag"},
	model.OpModifyConfig:   {baseCost: 5.0, category: "change"},
	model.OpScaleResource:  {baseCost: 5.0, category: "change"},
	model.OpRestartService: {baseCost: 10.0, category: "danger"},
	model.OpFixComplete:    {baseCost: 0.0, category: "info"},
	model.OpOther:          {baseCost: 2.0, category: "diag"},
}

// 重复操作惩罚倍率（按次数递增，第 1 次 1.0，第 2 次 1.5，第 3 次 2.0，封顶 3.0）。
var repeatMultipliers = []float64{1.0, 1.0, 1.5, 2.0, 2.5, 3.0}

// CostPenaltyConfig 操作代价计算配置。
type CostPenaltyConfig struct {
	// BlindActionThreshold 盲目操作判定阈值：观察类操作少于此数就执行变更，视为盲目。
	BlindActionThreshold int
	// MaxPenalty 最高惩罚分（封顶）。
	MaxPenalty float64
}

// DefaultCostConfig 默认配置。
func DefaultCostConfig() CostPenaltyConfig {
	return CostPenaltyConfig{
		BlindActionThreshold: 3,  // 至少观察 3 次再动手
		MaxPenalty:           80, // 最多扣 80 分（保底 20 分）
	}
}

// CalcCostPenalty 计算操作代价惩罚分。
//
// 计算过程：
//  1. 基础代价：按操作类型基础分 × 次数累加（重复操作有倍率惩罚）
//  2. 盲目操作惩罚：观察操作不足就执行变更/危险操作，每次额外惩罚
//  3. 总分封顶，不超过 MaxPenalty
func CalcCostPenalty(actions []model.OperationAction, cfg ...CostPenaltyConfig) CostResult {
	c := DefaultCostConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}

	result := CostResult{
		Breakdown: make(map[model.OperationType]OperationCostStat),
	}

	if len(actions) == 0 {
		return result
	}

	// 统计各类型操作次数
	typeCounts := make(map[model.OperationType]int)
	infoCount := 0       // 信息收集类操作数
	changeOrDanger := 0  // 变更/危险操作数
	blindViolations := 0 // 盲目操作次数

	// 按时间顺序遍历，检测盲目操作
	for _, action := range actions {
		typeCounts[action.Type]++
		cat := opCategory(action.Type)

		if cat == "info" {
			infoCount++
		}

		// 检测盲目操作：信息收集不足时就执行变更/危险操作
		if (cat == "change" || cat == "danger") && infoCount < c.BlindActionThreshold {
			blindViolations++
		}

		if cat == "change" || cat == "danger" {
			changeOrDanger++
		}
	}

	// 1. 计算基础代价 + 重复惩罚
	for opType, count := range typeCounts {
		cfg, ok := opCostConfig[opType]
		if !ok {
			cfg = opCostConfig[model.OpOther]
		}

		stat := OperationCostStat{Count: count}

		// 逐次计算，应用重复倍率
		totalBase := 0.0
		totalExtra := 0.0
		for i := 1; i <= count; i++ {
			mult := repeatMultiplier(i)
			base := cfg.baseCost
			totalBase += base
			totalExtra += base * (mult - 1.0) // 超出 1 倍的部分算额外惩罚
		}

		stat.BaseCost = round2(totalBase)
		stat.ExtraCost = round2(totalExtra)
		stat.TotalCost = round2(totalBase + totalExtra)
		result.Breakdown[opType] = stat
		result.BaseCost += stat.BaseCost
		result.RepeatPenalty += stat.ExtraCost
	}

	// 2. 盲目操作惩罚：每次违规扣 5 分
	blindPenaltyPerViolation := 5.0
	result.BlindPenalty = float64(blindViolations) * blindPenaltyPerViolation

	// 3. 总惩罚分
	totalPenalty := result.BaseCost + result.RepeatPenalty + result.BlindPenalty

	// 封顶
	if totalPenalty > c.MaxPenalty {
		totalPenalty = c.MaxPenalty
	}

	result.Penalty = round2(totalPenalty)

	// 消除未使用变量警告
	_ = changeOrDanger

	return result
}

// CostScore 操作代价得分（0-100） = 100 - Penalty。
func CostScore(actions []model.OperationAction, cfg ...CostPenaltyConfig) float64 {
	result := CalcCostPenalty(actions, cfg...)
	return clamp(100 - result.Penalty)
}

// ——— 内部工具函数 ———

// opCategory 获取操作分类。
func opCategory(t model.OperationType) string {
	if cfg, ok := opCostConfig[t]; ok {
		return cfg.category
	}
	return "diag"
}

// repeatMultiplier 获取第 n 次操作的重复惩罚倍率（n 从 1 开始）。
func repeatMultiplier(n int) float64 {
	if n <= 0 {
		return 1.0
	}
	if n > len(repeatMultipliers) {
		return repeatMultipliers[len(repeatMultipliers)-1]
	}
	return repeatMultipliers[n-1]
}
