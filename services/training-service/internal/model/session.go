// Package model 训练会话领域模型。
// training-service 是一次训练"全会话状态的唯一属主"：
// 沙箱、故障、AI、复盘都围绕会话状态被编排，但不持有会话状态。
package model

import "time"

// SessionStatus 训练会话状态。
type SessionStatus string

const (
	StatusPending   SessionStatus = "pending"   // 已创建，等待部署沙箱
	StatusDeploying SessionStatus = "deploying" // 沙箱部署中
	StatusArmed     SessionStatus = "armed"     // 就绪，故障已武装，等待触发
	StatusRunning   SessionStatus = "running"   // 实训进行中（故障可能已触发）
	StatusResolved  SessionStatus = "resolved"  // 故障已判定修复
	StatusCompleted SessionStatus = "completed" // 实训正常结束，复盘生成中/完成
	StatusAborted   SessionStatus = "aborted"   // 用户/系统主动终止（需释放沙箱）
	StatusFailed    SessionStatus = "failed"    // 异常失败（需释放沙箱）
)

// AIMode AI 参与模式（对应数据库 ai_mode）。
type AIMode string

const (
	AIModeNone   AIMode = "none"      // A0 纯人工
	AIModeAssist AIMode = "assistant" // A1 AI 助手
	// A2+ 自治模式为未来预留，MVP 不启用。
)

// ControlGroup 人机对照交叉分组（对应 control_group）。
type ControlGroup string

const (
	GroupA ControlGroup = "A" // 第一次人工 -> 第二次 AI
	GroupB ControlGroup = "B" // 第一次 AI -> 第二次人工
)

// Session 一次训练会话（training_sessions 聚合根）。
type Session struct {
	ID           int64         `json:"id"`
	UserID       int64         `json:"user_id"`
	ProjectID    int64         `json:"project_id"`
	SandboxID    int64         `json:"sandbox_id,omitempty"`
	Status       SessionStatus `json:"status"`
	AIMode       AIMode        `json:"ai_mode"`
	ControlGroup ControlGroup  `json:"control_group,omitempty"` // 首训前随机分配

	// PairSessionID 人机对照中与之配对的另一次会话（两次训练配对统计）。
	PairSessionID int64  `json:"pair_session_id,omitempty"`
	Difficulty    string `json:"difficulty,omitempty"` // simple/normal/hard

	// 双维评分：技术分由本服务计算，协作子分来自 ai-service，在此合并。
	TechScore     *float64 `json:"tech_score,omitempty"`
	CollabScore   *float64 `json:"collab_score,omitempty"`
	FaultResolved bool     `json:"fault_resolved"` // 故障是否被修复（vs 超时/放弃）

	StartedAt   *time.Time `json:"started_at,omitempty"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// CanStartNew 判断用户能否发起新会话（用于单用户并发控制在服务层判断，这里给状态语义）。
// 处于占用沙箱的活跃状态时不应再开新会话。
func (s *Session) OccupiesSandbox() bool {
	switch s.Status {
	case StatusDeploying, StatusArmed, StatusRunning, StatusResolved:
		return true
	default:
		return false
	}
}
