package repository

import (
	"context"
)

// Purge only runtime secrets and payment context; durable audit is intentionally retained.
func (s *PGStore) PurgeBrainRuntime(ctx context.Context, sid int64) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sandbox_training_audit WHERE session_id=$1 AND NOT delivered) OR EXISTS(SELECT 1 FROM behavior_commands WHERE session_id=$1 AND status IN ('succeeded','failed','unknown') AND NOT delivered)`, sid).Scan(&pending); err != nil {
		return false, err
	}
	if pending {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `DELETE FROM behavior_runner_credentials WHERE session_id=$1`, sid); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM sandbox_payments WHERE session_id=$1`, sid); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE behavior_commands SET status='cancelled',updated_at=now() WHERE session_id=$1 AND status IN ('accepted','running')`, sid); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
