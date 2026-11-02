// Package model 分析数据模型。
package model

import "time"

// ——— 事件类型定义 ———
// 训练漏斗核心事件（按顺序）：
//   register      用户注册
//   first_start   首次启动沙箱
//   first_complete 首次训练完成
//   retrain       复训（第二次及以后的训练）

const (
	EventRegister      = "register"       // 注册
	EventFirstStart    = "first_start"    // 首次启动沙箱
	EventFirstComplete = "first_complete" // 首次训练完成
	EventRetrain       = "retrain"        // 复训

	// 其他可扩展事件
	EventLogin            = "login"             // 登录
	EventViewProject      = "view_project"      // 查看项目
	EventStartTraining    = "start_training"    // 开始训练（每次）
	EventSubmitFix        = "submit_fix"        // 提交修复
	EventUseHint          = "use_hint"          // 使用提示
	EventAbandon          = "abandon"           // 放弃训练
	EventTrainingComplete = "training_complete" // 训练完成（每次，带时长）
	EventReviewRating     = "review_rating"     // 复盘评价（好评/差评）

	// ——— AI 行为事件（T4-14）———

	// 复盘相关
	EventReviewView  = "review_view"  // 查看复盘报告
	EventReviewShare = "review_share" // 分享复盘

	// 协作评估相关
	EventCollabView     = "collab_view"     // 查看协作评估板块
	EventCollabEvaluate = "collab_evaluate" // 提交协作评估
	EventCollabComment  = "collab_comment"  // 协作评论

	// 坑点相关
	EventPitfallEncounter = "pitfall_encounter" // 遇到坑点（触发）
	EventPitfallRecognize = "pitfall_recognize" // 识别坑点
	EventPitfallFall      = "pitfall_fall"      // 踩坑（受影响）

	// AI 对话相关
	EventAIChatMessage = "ai_chat_message" // AI 对话消息
	EventAIHintRequest = "ai_hint_request" // AI 提示请求
	EventAIExplanation = "ai_explanation"  // AI 讲解请求

	// ——— AI 专项看板事件（T5-8）———

	// 验证行为
	EventEvidenceVerify = "evidence_verify" // 证据验证行为（查日志/指标/实验）
	EventDangerousOp    = "dangerous_op"    // 危险操作（高代价操作）

	// 人机对照
	EventABComplete = "ab_complete" // 完成一组对照训练（A+B 两次）
)

// AIBehaviorEvents AI 行为相关事件列表。
var AIBehaviorEvents = []string{
	EventReviewView,
	EventReviewShare,
	EventCollabView,
	EventCollabEvaluate,
	EventCollabComment,
	EventPitfallEncounter,
	EventPitfallRecognize,
	EventPitfallFall,
	EventAIChatMessage,
	EventAIHintRequest,
	EventAIExplanation,
	EventEvidenceVerify,
	EventDangerousOp,
}

// Event 埋点事件。
type Event struct {
	ID        int64  `json:"id"`
	EventType string `json:"event_type"` // 事件类型
	UserID    int64  `json:"user_id"`    // 用户ID（匿名事件为 0）
	SessionID int64  `json:"session_id"` // 训练会话ID（可选）
	ProjectID int64  `json:"project_id"` // 项目ID（可选）

	// 事件属性（JSON 格式，灵活扩展）
	Properties map[string]interface{} `json:"properties,omitempty"`

	// 客户端信息
	ClientIP  string `json:"client_ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Platform  string `json:"platform,omitempty"` // web/mobile/desktop

	OccurredAt time.Time `json:"occurred_at"` // 事件发生时间
	ReceivedAt time.Time `json:"received_at"` // 服务端接收时间
}

// IsUniqueEvent 判断事件是否是"只记一次"的类型。
// 漏斗事件通常每个用户只记第一次。
var uniqueEvents = map[string]bool{
	EventRegister:      true,
	EventFirstStart:    true,
	EventFirstComplete: true,
}

// IsUniqueEvent 返回该事件类型是否每个用户只记录一次。
func IsUniqueEvent(eventType string) bool {
	return uniqueEvents[eventType]
}

// RegisterUniqueEvent 注册一个新的唯一事件类型。
func RegisterUniqueEvent(eventType string) {
	uniqueEvents[eventType] = true
}
