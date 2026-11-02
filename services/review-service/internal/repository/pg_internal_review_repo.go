// InternalReviewStore 的 PostgreSQL 实现：只读写协作子分白名单列。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 编译期断言：internalReviewRepo 满足 InternalReviewStore。
var _ InternalReviewStore = (*internalReviewRepo)(nil)

type internalReviewRepo struct {
	pool *pgxpool.Pool
}

// NewInternalReviewRepo 创建复盘协作数据内部读写实现。
func NewInternalReviewRepo(pool *pgxpool.Pool) InternalReviewStore {
	return &internalReviewRepo{pool: pool}
}

// GetCollabBySessionID 读取未删除复盘的协作子分；不存在返回 (nil, nil)。
// 列表白名单与原 training-service 共库 SELECT 口径一致，并补充 info_utilization_score。
func (r *internalReviewRepo) GetCollabBySessionID(
	ctx context.Context, sessionID int64,
) (*InternalReviewCollabView, error) {
	var v InternalReviewCollabView
	err := r.pool.QueryRow(ctx, `
		SELECT id,
		       session_id,
		       generation_status,
		       collab_score,
		       question_quality_score,
		       info_utilization_score,
		       judgment_score,
		       error_recognition_rate,
		       evidence_validation_rate,
		       ai_pitfall_count,
		       ai_pitfall_recognized
		  FROM reviews
		 WHERE session_id = $1
		   AND deleted_at IS NULL
	`, sessionID).Scan(
		&v.ID, &v.SessionID, &v.GenerationStatus,
		&v.CollabScore, &v.QuestionQualityScore, &v.InfoUtilizationScore,
		&v.JudgmentScore, &v.ErrorRecognitionRate, &v.EvidenceValidationRate,
		&v.AIPitfallCount, &v.AIPitfallRecognized,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// UpdateCollabScores 按会话 ID 更新协作子分；无未删除行时 found=false（不报错）。
// 更新列与原 training-service 共库 UPDATE 完全一致。
func (r *internalReviewRepo) UpdateCollabScores(
	ctx context.Context, sessionID int64, v InternalReviewCollabUpdate,
) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE reviews
		   SET collab_score = $1,
		       question_quality_score = $2,
		       info_utilization_score = $3,
		       judgment_score = $4,
		       error_recognition_rate = $5,
		       evidence_validation_rate = $6,
		       ai_pitfall_count = $7,
		       ai_pitfall_recognized = $8,
		       updated_at = now()
		 WHERE session_id = $9
		   AND deleted_at IS NULL
	`,
		v.CollabScore, v.QuestionQualityScore, v.InfoUtilizationScore,
		v.JudgmentScore, v.ErrorRecognitionRate, v.EvidenceValidationRate,
		v.AIPitfallCount, v.AIPitfallRecognized, sessionID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ListExistence 批量查询会话对应的复盘 id；无复盘（含软删）时 review_id 为 NULL。
// 结果顺序不保证，调用方按 session_id 建映射。
func (r *internalReviewRepo) ListExistence(
	ctx context.Context, sessionIDs []int64,
) ([]InternalReviewExistence, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.session_id, rv.id
		  FROM unnest($1::bigint[]) AS t(session_id)
		  LEFT JOIN reviews rv
		    ON rv.session_id = t.session_id
		   AND rv.deleted_at IS NULL
	`, sessionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]InternalReviewExistence, 0, len(sessionIDs))
	for rows.Next() {
		var item InternalReviewExistence
		if err := rows.Scan(&item.SessionID, &item.ReviewID); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
