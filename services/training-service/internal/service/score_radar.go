package service

import "github.com/quenching/training-service/internal/model"

// CollabRadarDimensions 协作维雷达的 6 个维度（顺序固定，与前端对齐）。
var CollabRadarDimensions = []string{
	"question_quality",   // 提问质量
	"info_utilization",   // 信息利用率
	"judgment",           // 判断力
	"pitfall_awareness",  // 坑点意识
	"verification_habit", // 验证习惯
	"independence",       // 独立性
}

// TechRadarDimensions 技术维雷达的 6 个维度（顺序固定，与前端对齐）。
var TechRadarDimensions = []string{
	"fault_diagnosis",  // 故障诊断
	"tool_proficiency", // 工具熟练度
	"info_retrieval",   // 信息检索
	"operation_cost",   // 操作代价
	"verification",     // 验证习惯
	"time_efficiency",  // 时间效率
}

// CollabRadarInput 构造协作维雷达所需的原始输入。
// 各项均为 0-100 分量；缺失项传 nil 时按"不参与本次展示"处理（值为 0，
// 但 has_collab=false 时整体雷达不显示）。independence 为独立性，
// 由编排层从提问次数/提示依赖/自主修复情况推导。
type CollabRadarInput struct {
	QuestionQuality   float64 // 提问质量
	InfoUtilization   float64 // 信息利用率
	Judgment          float64 // 判断力综合
	PitfallAwareness  float64 // 坑点意识（错误建议识别率）
	VerificationHabit float64 // 验证习惯（证据验证率）
	Independence      float64 // 独立性
}

// BuildCollabRadar 从三维子分+补充维度构造协作维雷达数据。
// 维度顺序与 CollabRadarDimensions 一致。
func BuildCollabRadar(in CollabRadarInput) model.RadarData {
	values := make([]float64, len(CollabRadarDimensions))
	values[0] = clamp(in.QuestionQuality)
	values[1] = clamp(in.InfoUtilization)
	values[2] = clamp(in.Judgment)
	values[3] = clamp(in.PitfallAwareness)
	values[4] = clamp(in.VerificationHabit)
	values[5] = clamp(in.Independence)
	return model.RadarData{
		Dimensions: append([]string{}, CollabRadarDimensions...),
		Values:     values,
	}
}

// DualDimensionRadar 双维能力雷达完整数据（一次训练的结果）。
// 与前端 DualDimensionRadar 类型对齐。
type DualDimensionRadar struct {
	CurrentTech   model.RadarData  `json:"current_tech"`
	CurrentCollab model.RadarData  `json:"current_collab"`
	AvgTech       *model.RadarData `json:"avg_tech,omitempty"`
	AvgCollab     *model.RadarData `json:"avg_collab,omitempty"`
	TechOverall   float64          `json:"tech_overall"`
	CollabOverall float64          `json:"collab_overall"`
	HasCollab     bool             `json:"has_collab"`
	Overall       float64          `json:"overall"`
	Level         string           `json:"level"`
}

// BuildDualRadar 构造双维雷达数据。
// techRadar / collabRadar 分别由调用方组装（技术维在 T4 补齐，
// MVP 先用占位维度）；overall 与 level 取自 Result。
func BuildDualRadar(techRadar, collabRadar model.RadarData, res Result) DualDimensionRadar {
	return DualDimensionRadar{
		CurrentTech:   techRadar,
		CurrentCollab: collabRadar,
		TechOverall:   res.Tech,
		CollabOverall: res.Collab,
		HasCollab:     res.HasCollab,
		Overall:       res.Overall,
		Level:         res.Level,
	}
}

// BuildTechRadar 从技术分输入构造技术维雷达（MVP 简化版）。
//
// MVP 阶段技术维只做总分，6 个子维度为粗略映射：
//   - fault_diagnosis: 以是否修复为基准（占技术分权重最高）
//   - tool_proficiency: 操作代价反向（低代价 = 熟练）
//   - info_retrieval: 与提示次数反向（少提示 = 信息检索强）
//   - operation_cost: 操作代价得分
//   - verification: 有修复则高（修复=验证了方案）
//   - time_efficiency: 耗时得分
//
// 后续 T4 技术雷达细化时替换此函数，上层调用不变。
func BuildTechRadar(techIn TechInput, w Weights) model.RadarData {
	values := make([]float64, len(TechRadarDimensions))

	// 基础分
	base := w.ResolvedBase
	if !techIn.FaultResolved {
		base = w.UnresolvedBase
	}

	// 各维度粗略映射（MVP 简化，保证总分 ≈ techScore）
	values[0] = clamp(base)                     // fault_diagnosis：是否修复
	values[1] = clamp(100 - techIn.CostPenalty) // tool_proficiency：操作代价反向
	// info_retrieval：提示次数反向（hint=0 得 100，每用一次扣 hintPenaltyPerUse）
	hintPenalty := float64(techIn.HintCount) * hintPenaltyPerUse
	if hintPenalty > maxHintPenalty {
		hintPenalty = maxHintPenalty
	}
	values[2] = clamp(100 - hintPenalty)
	values[3] = clamp(100 - techIn.CostPenalty) // operation_cost
	values[4] = clamp(base)                     // verification：修复即验证了
	// time_efficiency：耗时 vs 预期
	timeScore := 100.0
	if techIn.ExpectedSec > 0 && techIn.DurationSec > 0 {
		ratio := float64(techIn.ExpectedSec) / float64(techIn.DurationSec)
		if ratio > 1 {
			timeScore = 100 // 比预期快，满分
		} else {
			timeScore = clamp(ratio * 100)
		}
	}
	values[5] = timeScore

	return model.RadarData{
		Dimensions: append([]string{}, TechRadarDimensions...),
		Values:     values,
	}
}
