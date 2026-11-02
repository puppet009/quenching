// Package repository 数据访问层：只做读写，不含业务规则。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/review-service/internal/model"
)

// ErrNotFound 数据不存在。
var ErrNotFound = errors.New("record not found")

// ReviewRepo 复盘数据访问。
type ReviewRepo struct {
	pool *pgxpool.Pool
}

// NewReviewRepo 创建。
func NewReviewRepo(pool *pgxpool.Pool) *ReviewRepo {
	return &ReviewRepo{pool: pool}
}

// Create 插入复盘记录（初始状态 pending）。
// 冲突（同 session_id 已存在）返回错误。
func (r *ReviewRepo) Create(ctx context.Context, rv *model.Review) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO reviews (session_id, user_id, project_id, review_level, generation_status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (session_id) DO NOTHING
		RETURNING id, created_at, updated_at`,
		rv.SessionID, rv.UserID, rv.ProjectID, rv.ReviewLevel, rv.GenerationStatus,
	).Scan(&rv.ID, &rv.CreatedAt, &rv.UpdatedAt)
}

// GetByID 按 ID 查。
func (r *ReviewRepo) GetByID(ctx context.Context, id int64) (*model.Review, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, session_id, user_id, project_id, COALESCE(title,''), COALESCE(summary,''), COALESCE(overall_evaluation,''),
			review_level, generation_status, COALESCE(ai_model,''), COALESCE(prompt_version,''), COALESCE(generation_duration,0),
			COALESCE(tech_score,0), COALESCE(collab_score,0), COALESCE(question_quality_score,0), COALESCE(judgment_score,0),
			COALESCE(error_recognition_rate,0), COALESCE(evidence_validation_rate,0), ai_pitfall_count,
			ai_pitfall_recognized, created_at, updated_at
		FROM reviews WHERE id = $1 AND deleted_at IS NULL`, id)
	return scanReview(row)
}

// GetBySessionID 按会话 ID 查。
func (r *ReviewRepo) GetBySessionID(ctx context.Context, sessionID int64) (*model.Review, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, session_id, user_id, project_id, COALESCE(title,''), COALESCE(summary,''), COALESCE(overall_evaluation,''),
			review_level, generation_status, COALESCE(ai_model,''), COALESCE(prompt_version,''), COALESCE(generation_duration,0),
			COALESCE(tech_score,0), COALESCE(collab_score,0), COALESCE(question_quality_score,0), COALESCE(judgment_score,0),
			COALESCE(error_recognition_rate,0), COALESCE(evidence_validation_rate,0), ai_pitfall_count,
			ai_pitfall_recognized, created_at, updated_at
		FROM reviews WHERE session_id = $1 AND deleted_at IS NULL`, sessionID)
	return scanReview(row)
}

// ListByUser 分页查询某用户的复盘列表（T4-16 历史接口）。
// 只取列表页需要的轻量列，不取 summary/overall_evaluation 大 TEXT；
// 排序 created_at DESC, id DESC 与部分索引 idx_reviews_user_created 对齐，
// 分页稳定（同时间用 id 兜底，避免翻页丢行/重复行）。
func (r *ReviewRepo) ListByUser(ctx context.Context, userID int64, limit, offset int) ([]model.ReviewListItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, session_id, COALESCE(title, ''), generation_status,
			COALESCE(tech_score, 0), COALESCE(collab_score, 0),
			COALESCE(generation_duration, 0), created_at
		FROM reviews
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.ReviewListItem, 0)
	for rows.Next() {
		var item model.ReviewListItem
		if err := rows.Scan(
			&item.ID, &item.SessionID, &item.Title, &item.GenerationStatus,
			&item.TechScore, &item.CollabScore, &item.GenerationDuration,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CountByUser 统计某用户的复盘总数（T4-16 历史接口分页用）。
func (r *ReviewRepo) CountByUser(ctx context.Context, userID int64) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx, `
		SELECT count(1) FROM reviews
		WHERE user_id = $1 AND deleted_at IS NULL`, userID).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total, nil
}

// GetStatusBySessionID 按会话 ID 查询生成状态（T4-16 前端轮询专用，轻量列）。
func (r *ReviewRepo) GetStatusBySessionID(ctx context.Context, sessionID int64) (*model.ReviewStatus, error) {
	st := &model.ReviewStatus{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, session_id, generation_status, COALESCE(error_message, '')
		FROM reviews
		WHERE session_id = $1 AND deleted_at IS NULL`, sessionID).
		Scan(&st.ID, &st.SessionID, &st.GenerationStatus, &st.ErrorMessage)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return st, nil
}

// MarkGenerating 标记为生成中（+ 重置错误信息）。
func (r *ReviewRepo) MarkGenerating(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE reviews SET generation_status = $1, error_message = '', updated_at = now()
		WHERE id = $2`, model.GenStatusGenerating, id)
	return err
}

// MarkCompleted 标记为完成，回写结果 + 坑点统计（T3-13）。
func (r *ReviewRepo) MarkCompleted(ctx context.Context, id int64, title, summary, overallEval string, durationMs int, techScore, collabScore float64, pitfallCount, pitfallRecognized int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE reviews SET
			generation_status = $1,
			title = $2,
			summary = $3,
			overall_evaluation = $4,
			generation_duration = $5,
			tech_score = CASE WHEN $6 <> 0 THEN $6 ELSE tech_score END,
			collab_score = CASE WHEN $7 <> 0 THEN $7 ELSE collab_score END,
			ai_pitfall_count = $8,
			ai_pitfall_recognized = $9,
			updated_at = now()
		WHERE id = $10`,
		model.GenStatusCompleted, title, summary, overallEval, durationMs,
		techScore, collabScore, pitfallCount, pitfallRecognized, id)
	return err
}

// MarkFailed 标记为失败，记录错误信息。
func (r *ReviewRepo) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE reviews SET generation_status = $1, error_message = $2, updated_at = now()
		WHERE id = $3`, model.GenStatusFailed, errMsg, id)
	return err
}

// IncrementRetry 增加重试次数（返回当前重试次数）。
func (r *ReviewRepo) IncrementRetry(ctx context.Context, id int64) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		UPDATE reviews SET retry_count = retry_count + 1, updated_at = now()
		WHERE id = $1 RETURNING retry_count`, id).Scan(&count)
	return count, err
}

// scanReview 扫描一行 Review。
func scanReview(row pgx.Row) (*model.Review, error) {
	rv := &model.Review{}
	err := row.Scan(
		&rv.ID, &rv.SessionID, &rv.UserID, &rv.ProjectID,
		&rv.Title, &rv.Summary, &rv.OverallEvaluation,
		&rv.ReviewLevel, &rv.GenerationStatus, &rv.AIModel, &rv.PromptVersion,
		&rv.GenerationDuration,
		&rv.TechScore, &rv.CollabScore, &rv.QuestionQualityScore, &rv.JudgmentScore,
		&rv.ErrorRecognitionRate, &rv.EvidenceValidationRate,
		&rv.AIPitfallCount, &rv.AIPitfallRecognized,
		&rv.CreatedAt, &rv.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rv, nil
}
