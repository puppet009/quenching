// 双维评分规则（纯函数，无外部依赖，可完整单测）。
//
// 两个维度：
//   - 技术分 TechScore：训练行为客观计算（是否修复、耗时、操作代价、提示依赖）。
//   - 协作分 CollabScore：来自 ai-service 的三维子分（提问质量/信息利用率/判断力），
//     training-service 只做加权合并，不在此重新评判 AI 交互内容。
//
// 权重、阈值集中在 Weights，方便运营调参而不改逻辑。
package service

// Result 双维评分结果。
type Result struct {
	Tech      float64 `json:"tech"`       // 技术分 0-100
	Collab    float64 `json:"collab"`     // 协作分 0-100（无协作数据时为 0，HasCollab=false）
	HasCollab bool    `json:"has_collab"` // 是否存在有效协作分
	Overall   float64 `json:"overall"`    // 综合分（双维加权）
	Level     string  `json:"level"`      // 协作等级 C1-C5
}

// Evaluate 计算完整双维评分结果。
// techIn 为本服务统计的训练行为，collab 为 ai-service 提供的协作子分。
func Evaluate(techIn TechInput, collab CollabSubScores, w Weights) Result {
	tech := TechScore(techIn, w)
	collabScore, hasCollab := CollabScore(collab, w)

	res := Result{
		Tech:      round1(tech),
		Collab:    0,
		HasCollab: hasCollab,
		Level:     collabLevel(collabScore, hasCollab),
	}

	if hasCollab {
		res.Collab = round1(collabScore)
		res.Overall = round1(tech*w.OverallTechWeight + collabScore*w.OverallCollabWeight)
	} else {
		// 纯人工模式：综合分即技术分，不被空协作分拉低。
		res.Overall = round1(tech)
	}
	return res
}

// round1 保留一位小数。
func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
