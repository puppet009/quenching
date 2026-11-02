// 服务间内部查询专用的复盘协作子分投影与端口。
//
// training-service 不再直接读取或写入 reviews，统一经 review-service 的
// /internal/v1 接口访问。投影只暴露协作评分相关字段，不包含 summary、
// overall_evaluation、share_token、user_feedback 等复盘正文或分享域字段。
package repository

import (
	"context"
)

// InternalReviewCollabView 复盘协作子分最小投影。
// 指针字段表示 reviews 中可空的子分列，无复盘行时整体为 nil。
type InternalReviewCollabView struct {
	ID                     int64    `json:"id"`
	SessionID              int64    `json:"session_id"`
	GenerationStatus       string   `json:"generation_status"`
	CollabScore            *float64 `json:"collab_score"`
	QuestionQualityScore   *float64 `json:"question_quality_score"`
	InfoUtilizationScore   *float64 `json:"info_utilization_score"`
	JudgmentScore          *float64 `json:"judgment_score"`
	ErrorRecognitionRate   *float64 `json:"error_recognition_rate"`
	EvidenceValidationRate *float64 `json:"evidence_validation_rate"`
	AIPitfallCount         *int     `json:"ai_pitfall_count"`
	AIPitfallRecognized    *int     `json:"ai_pitfall_recognized"`
}

// InternalReviewExistence 复盘存在性批量结果项。
type InternalReviewExistence struct {
	SessionID int64  `json:"session_id"`
	ReviewID  *int64 `json:"review_id"`
}

// InternalReviewStore 复盘协作数据的服务间读写端口。
type InternalReviewStore interface {
	// GetCollabBySessionID 按会话 ID 读取协作子分；复盘不存在时返回 (nil, nil)。
	GetCollabBySessionID(ctx context.Context, sessionID int64) (*InternalReviewCollabView, error)
	// UpdateCollabScores 按会话 ID 更新协作子分；found=false 表示无未删除复盘行。
	UpdateCollabScores(
		ctx context.Context, sessionID int64, v InternalReviewCollabUpdate,
	) (found bool, err error)
	// ListExistence 按会话 ID 集合批量返回复盘 id；软删复盘不返回。
	ListExistence(ctx context.Context, sessionIDs []int64) ([]InternalReviewExistence, error)
}

// InternalReviewCollabUpdate training-service 提交的协作子分更新值。
type InternalReviewCollabUpdate struct {
	CollabScore            float64
	QuestionQualityScore   float64
	InfoUtilizationScore   float64
	JudgmentScore          float64
	ErrorRecognitionRate   float64
	EvidenceValidationRate float64
	AIPitfallCount         int
	AIPitfallRecognized    int
}
