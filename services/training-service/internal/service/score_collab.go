package service

// CollabSubScores ai-service 提供的协作三维子分（每项 0-100）。
// 对应 MVP 三维协作指数：提问质量 + 信息利用率 + 判断力（含错误建议识别率/证据验证率）。
type CollabSubScores struct {
	QuestionQuality float64 // 提问质量
	InfoUtilization float64 // 信息利用率
	Judgment        float64 // 判断力
	// Available 本次训练是否有 AI 协作数据（纯人工 A0 模式为 false）。
	Available bool
}

// CollabScore 加权合并 ai-service 的三维协作子分。
// 返回 (分数, 是否有效)：纯人工模式/无协作数据时第二值为 false。
func CollabScore(s CollabSubScores, w Weights) (float64, bool) {
	if !s.Available {
		return 0, false
	}
	total := s.QuestionQuality*w.CollabQuestion +
		s.InfoUtilization*w.CollabInfo +
		s.Judgment*w.CollabJudgment
	return clamp(total), true
}

// collabLevel 按协作分映射 C1-C5（对应数据库 collab_level）。
func collabLevel(score float64, has bool) string {
	if !has {
		return "C1" // 无协作数据视为初始级
	}
	switch {
	case score >= 85:
		return "C5"
	case score >= 70:
		return "C4"
	case score >= 55:
		return "C3"
	case score >= 40:
		return "C2"
	default:
		return "C1"
	}
}
