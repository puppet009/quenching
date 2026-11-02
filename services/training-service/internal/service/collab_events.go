// 协作行为事件采集 DTO（T3-24 迁移）。
//
// 归属约定：协作三维子分由 ai-service 的确定性规则计算（见 packages/proto
// 的 EvaluateCollab），training-service 只负责从自有三表采集事件、加权合并与回写。
// 本文件是仓储层与编排层之间的传输结构：repository 聚合事件后返回这些 DTO，
// 编排层再映射为 proto 请求发往 ai-service；仓储层因此不依赖 proto。
package service

// PitfallOutcome 单个坑点的用户反应（来自坑点判定 agree/dispute/verified/ignore）。
type PitfallOutcome string

const (
	// PitfallOutcomeRecognized 识别了坑（反驳/绕行/独立验证后走对：dispute/verified）。
	PitfallOutcomeRecognized PitfallOutcome = "recognized"
	// PitfallOutcomeFell 踩坑（采纳了 AI 错误建议：agree/fell）。
	PitfallOutcomeFell PitfallOutcome = "fell"
	// PitfallOutcomePending 未遭遇/未判定（不计入分母）。
	PitfallOutcomePending PitfallOutcome = "pending"
)

// CollabActionKind 操作类型。
type CollabActionKind string

const (
	// CollabActionFollowAI 采纳 AI 建议执行的操作。
	CollabActionFollowAI CollabActionKind = "follow_ai"
	// CollabActionIndependent 自主排查操作（查日志/指标/实验）。
	CollabActionIndependent CollabActionKind = "independent"
)

// CollabQuestionEvent 一条用户提问。
type CollabQuestionEvent struct {
	Text                string
	Stage               string
	ReferencesKnownInfo bool
	FollowUp            bool
}

// CollabPitfallEvent 一次坑点遭遇的判定结果。
type CollabPitfallEvent struct {
	Outcome PitfallOutcome
}

// CollabActionEvent 一条排查操作（来自操作时间线）。
type CollabActionEvent struct {
	Kind                 CollabActionKind
	VerifiedBeforeFollow bool
}

// CollabEvents 一次会话采集到的全部协作事件。
type CollabEvents struct {
	Questions []CollabQuestionEvent
	Pitfalls  []CollabPitfallEvent
	Actions   []CollabActionEvent
}
