package repository

import (
	"context"

	"github.com/quenching/training-service/internal/service"
)

// Only this service's terminal sessions are scanned. Review existence is read
// through review-service, never joined across business ownership boundaries.
func (r *SessionRepo) ListClosedLegacySessions(ctx context.Context, afterID int64, limit int) ([]service.ClosedLegacySession, error) {
	if limit < 1 || limit > 32 {
		limit = 32
	}
	rows, err := r.pool.Query(ctx, `SELECT s.id,s.user_id,s.project_id,s.status
 FROM training_sessions s
 WHERE s.id>$1 AND s.deleted_at IS NULL AND s.project_id>0 AND s.ended_at IS NOT NULL
 AND coalesce(s.template_slug,'')=''
 AND s.status IN ('completed','aborted','failed')
 AND NOT EXISTS(SELECT 1 FROM brain_sessions b WHERE b.session_id=s.id)
 ORDER BY s.id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []service.ClosedLegacySession
	for rows.Next() {
		var row service.ClosedLegacySession
		if err = rows.Scan(&row.ID, &row.UserID, &row.ProjectID, &row.Status); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
