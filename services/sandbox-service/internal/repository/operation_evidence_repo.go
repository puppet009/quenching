package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	oe "github.com/quenching/proto/operationevidence"
	operationaudit "github.com/quenching/sandbox-service/internal/repository/operationaudit"
)

func NewOperationStore(p *pgxpool.Pool) *operationaudit.PGStore {
	return &operationaudit.PGStore{Pool: p, Local: false, Sandbox: true, Guard: func(ctx context.Context, tx pgx.Tx, sid int64) error {
		var open bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sandboxes WHERE session_id=$1 AND completed_at IS NULL AND status IN ('ready','running') AND deleted_at IS NULL)`, sid).Scan(&open)
		if err != nil {
			return err
		}
		if !open {
			return oe.ErrClosed
		}
		return nil
	}}
}
func OperationOwner(p *pgxpool.Pool) func(context.Context, int64, int64) error {
	return func(ctx context.Context, uid, sid int64) error {
		var exists bool
		err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sandboxes WHERE session_id=$1 AND user_id=$2 AND deleted_at IS NULL)`, sid, uid).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return oe.ErrNotFound
		}
		return nil
	}
}
