package repository

import (
	"context"
	"errors"
)

// Serialize the authorization create step with completion, even when no
// namespace exists yet. MarkCompleted's UPDATE acquires the same row lock.
func (s *PGStore) WithWorkspaceAccess(ctx context.Context, sid int64, ensure func(context.Context) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var open bool
	err = tx.QueryRow(ctx, `SELECT completed_at IS NULL AND deleted_at IS NULL
		AND status IN ('creating','deploying','ready','running')
		FROM sandboxes WHERE session_id=$1 ORDER BY id DESC LIMIT 1 FOR UPDATE`, sid).Scan(&open)
	if err != nil {
		return err
	}
	if !open {
		return errors.New("training workspace is closed for authorization")
	}
	if err := ensure(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
