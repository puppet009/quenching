// Package model 复盘领域模型。
package model

import "time"

// 复盘生成状态。
const (
	GenStatusPending    = "pending"    // 等待生成（已投递队列）
	GenStatusGenerating = "generating" // 生成中
	GenStatusCompleted  = "completed"  // 已完成
	GenStatusFailed     = "failed"     // 生成失败
)

// 复盘级别。
const (
	ReviewLevelBasic  = "basic"  // 基础版（迭代2）
	ReviewLevelMethod = "method" // 方法论版（迭代3）
	ReviewLevelArch   = "arch"   // 架构版（后续）
)

// Review 复盘报告。
// 对应 reviews 表。一会话一份复盘，session_id 唯一。
type Review struct {
	ID                     int64     `json:"id"`
	SessionID              int64     `json:"session_id"`
	UserID                 int64     `json:"user_id"`
	ProjectID              int64     `json:"project_id"`
	Title                  string    `json:"title"`
	Summary                string    `json:"summary"`
	OverallEvaluation      string    `json:"overall_evaluation"`
	ReviewLevel            string    `json:"review_level"`
	GenerationStatus       string    `json:"generation_status"`
	AIModel                string    `json:"ai_model"`
	PromptVersion          string    `json:"prompt_version"`
	GenerationDuration     int       `json:"generation_duration_ms"` // 毫秒
	TechScore              float64   `json:"tech_score"`
	CollabScore            float64   `json:"collab_score"`
	QuestionQualityScore   float64   `json:"question_quality_score"`
	JudgmentScore          float64   `json:"judgment_score"`
	ErrorRecognitionRate   float64   `json:"error_recognition_rate"`
	EvidenceValidationRate float64   `json:"evidence_validation_rate"`
	AIPitfallCount         int       `json:"ai_pitfall_count"`
	AIPitfallRecognized    int       `json:"ai_pitfall_recognized"`
	RetryCount             int       `json:"retry_count"` // 重试次数
	ErrorMessage           string    `json:"error_message"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// ReviewListItem 复盘历史列表项（T4-16）。
// 列表页只需状态/评分/耗时等摘要字段，刻意不取 summary/overall_evaluation
// 等大 TEXT，避免长列表回表放大；点进详情再走 GetByID。
type ReviewListItem struct {
	ID                 int64     `json:"id"`
	SessionID          int64     `json:"session_id"`
	Title              string    `json:"title"`
	GenerationStatus   string    `json:"generation_status"`
	TechScore          float64   `json:"tech_score"`
	CollabScore        float64   `json:"collab_score"`
	GenerationDuration int       `json:"generation_duration_ms"` // 毫秒
	CreatedAt          time.Time `json:"created_at"`
}

// ReviewStatus 复盘生成状态轮询结果（T4-16）。
// 前端触发生成后按 session_id 轮询，只需状态字段，不拉大字段。
type ReviewStatus struct {
	Phase            string `json:"phase,omitempty"`
	ProcessedSources int64  `json:"processed_sources,omitempty"`
	TotalSources     int64  `json:"total_sources,omitempty"`
	ID               int64  `json:"id"`
	SessionID        int64  `json:"session_id"`
	GenerationStatus string `json:"generation_status"`
	ErrorMessage     string `json:"error_message"`
}
