package repository

import (
	"context"
	"github.com/quenching/training-service/internal/service"
)

func (r *BrainRepo) CleanupState(ctx context.Context, sid int64) (service.BrainCleanupState, error) {
	var s service.BrainCleanupState
	err := r.pool.QueryRow(ctx, `SELECT s.user_id,coalesce(s.project_id,0),b.report_requested FROM brain_sessions b JOIN training_sessions s ON s.id=b.session_id WHERE b.session_id=$1`, sid).Scan(&s.UserID, &s.ProjectID, &s.ReportRequested)
	return s, err
}
func (r *BrainRepo) RecordCleanup(ctx context.Context, sid int64, status, reason string) error {
	_, err := r.pool.Exec(ctx, `UPDATE brain_sessions SET cleanup_status=$2,cleanup_reason=$3,cleanup_retry_at=now()+interval '30 seconds' WHERE session_id=$1`, sid, status, reason)
	return err
}
func (r *BrainRepo) MarkReportRequested(ctx context.Context, sid int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE brain_sessions SET report_requested=true,phase='reviewing' WHERE session_id=$1 AND phase IN ('closing','reviewing')`, sid)
	return err
}
func (r *BrainRepo) ClaimClosingSessions(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `WITH candidates AS (SELECT session_id FROM brain_sessions WHERE (phase IN ('closing','reviewing') OR (phase IN ('completed','failed') AND cleanup_status<>'succeeded')) AND (cleanup_lease_until IS NULL OR cleanup_lease_until<now()) AND (cleanup_retry_at IS NULL OR cleanup_retry_at<now()) ORDER BY session_id LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE brain_sessions b SET cleanup_lease_until=now()+interval '60 seconds' FROM candidates c WHERE b.session_id=c.session_id RETURNING b.session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var sid int64
		if err = rows.Scan(&sid); err != nil {
			return nil, err
		}
		ids = append(ids, sid)
	}
	return ids, rows.Err()
}
func (r *BrainRepo) ReleaseCleanup(ctx context.Context, sid int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE brain_sessions SET cleanup_lease_until=NULL,cleanup_retry_at=now()+interval '30 seconds' WHERE session_id=$1`, sid)
	return err
}
func (r *BrainRepo) RecordReportStatus(ctx context.Context, sid int64, status string) error {
	if status != "completed" && status != "failed" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE brain_sessions SET phase=$2 WHERE session_id=$1 AND report_requested`, sid, status)
	return err
}
