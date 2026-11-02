package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/training-service/internal/model"
	"time"
)

type FeedbackRepo struct{ pool *pgxpool.Pool }

func NewFeedbackRepo(pool *pgxpool.Pool) *FeedbackRepo { return &FeedbackRepo{pool: pool} }
func (r *FeedbackRepo) Save(ctx context.Context, uid int64, in model.FeedbackInput) (int64, error) {
	var id int64
	// Ownership and existence are checked in the same statement as the write.
	err := r.pool.QueryRow(ctx, `INSERT INTO training_feedback(training_id,user_id,score,value_feedback,comment)
 SELECT id,user_id,$3,$4,COALESCE($5,'') FROM training_sessions
 WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL
 ON CONFLICT(user_id,training_id) DO UPDATE SET
 score=COALESCE(EXCLUDED.score,training_feedback.score),
 value_feedback=COALESCE(EXCLUDED.value_feedback,training_feedback.value_feedback),
 comment=CASE WHEN $5::text IS NULL THEN training_feedback.comment ELSE EXCLUDED.comment END,updated_at=now()
 RETURNING id`, in.TrainingID, uid, in.Score, in.ValueFeedback, in.Comment).Scan(&id)
	return id, err
}
func (r *FeedbackRepo) Stats(ctx context.Context) (map[string]interface{}, error) {
	var total, promoters, passives, detractors, valuable, notValuable int
	var avg *float64
	err := r.pool.QueryRow(ctx, `SELECT count(f.score),count(*) FILTER(WHERE f.score>=9),
 count(*) FILTER(WHERE f.score BETWEEN 7 AND 8),count(*) FILTER(WHERE f.score<=6),
 count(*) FILTER(WHERE f.value_feedback='valuable'),count(*) FILTER(WHERE f.value_feedback='not_valuable'),avg(f.score)::float8
 FROM training_feedback f JOIN training_sessions t ON t.id=f.training_id WHERE t.deleted_at IS NULL`).Scan(&total, &promoters, &passives, &detractors, &valuable, &notValuable, &avg)
	if err != nil {
		return nil, err
	}
	var nps interface{} = nil
	if total > 0 {
		nps = float64(promoters-detractors) * 100 / float64(total)
	}
	return map[string]interface{}{"total_count": total, "nps_score": nps, "promoters": promoters, "passives": passives, "detractors": detractors, "valuable_count": valuable, "not_valuable_count": notValuable, "avg_score": avg}, nil
}
func (r *FeedbackRepo) List(ctx context.Context, page, size, min, max int, hasComment *bool) (map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}
	args := []interface{}{min, max}
	where := "WHERE t.deleted_at IS NULL AND f.score BETWEEN $1 AND $2"
	if hasComment != nil {
		where += " AND (length(trim(f.comment))>0)=$3"
		args = append(args, *hasComment)
	}
	var total int
	err := r.pool.QueryRow(ctx, "SELECT count(*) FROM training_feedback f JOIN training_sessions t ON t.id=f.training_id "+where, args...).Scan(&total)
	if err != nil {
		return nil, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.pool.Query(ctx, `SELECT f.id,f.user_id,f.training_id,f.score,COALESCE(f.value_feedback,''),f.comment,f.created_at,
 COALESCE(NULLIF(t.title,''),NULLIF(t.template_slug,''),'训练 #'||t.id::text)
 FROM training_feedback f JOIN training_sessions t ON t.id=f.training_id `+where+fmt.Sprintf(" ORDER BY f.created_at DESC,f.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, uid, tid int64
		var score int
		var value, comment, title string
		var created time.Time
		if err := rows.Scan(&id, &uid, &tid, &score, &value, &comment, &created, &title); err != nil {
			return nil, err
		}
		items = append(items, map[string]interface{}{"id": id, "user_id": uid, "user_name": fmt.Sprintf("用户 #%d", uid), "training_id": tid, "project_name": title, "defect_name": "", "score": score, "value_feedback": value, "comment": comment, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	stats, err := r.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"items": items, "total": total, "stats": stats}, nil
}
