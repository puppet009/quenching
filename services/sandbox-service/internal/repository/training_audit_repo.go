package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/sandbox-service/internal/service"
)

type TrainingAuditRepo struct{ pool *pgxpool.Pool }

func NewTrainingAuditRepo(p *pgxpool.Pool) *TrainingAuditRepo { return &TrainingAuditRepo{p} }
func (r *TrainingAuditRepo) AppendAudit(ctx context.Context, id string, sid int64, kind string, event []byte) error {
	_, e := r.pool.Exec(ctx, `INSERT INTO sandbox_training_audit(id,session_id,kind,event) SELECT $1,$2,$3,$4 FROM sandboxes WHERE session_id=$2 AND template_slug IS NOT NULL AND template_slug<>'' LIMIT 1 ON CONFLICT DO NOTHING`, id, sid, kind, event)
	return e
}
func (r *TrainingAuditRepo) PendingAudits(ctx context.Context) ([]service.TrainingAuditDelivery, error) {
	rows, e := r.pool.Query(ctx, `SELECT id,event FROM sandbox_training_audit WHERE NOT delivered ORDER BY created_at LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []service.TrainingAuditDelivery{}
	for rows.Next() {
		var v service.TrainingAuditDelivery
		if e = rows.Scan(&v.ID, &v.Event); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *TrainingAuditRepo) MarkAuditDelivered(ctx context.Context, id string) error {
	_, e := r.pool.Exec(ctx, `UPDATE sandbox_training_audit SET delivered=true WHERE id=$1`, id)
	return e
}

func (r *TrainingAuditRepo) WorkspaceOpen(ctx context.Context, sid int64) (bool, error) {
	var open bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sandboxes WHERE session_id=$1 AND completed_at IS NULL AND status IN ('ready','running') AND deleted_at IS NULL)`, sid).Scan(&open)
	return open, err
}
