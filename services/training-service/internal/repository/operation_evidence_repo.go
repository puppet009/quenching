package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	oe "github.com/quenching/proto/operationevidence"
	tooljournal "github.com/quenching/training-service/internal/repository/tooljournal"
)

func NewToolJournal(p *pgxpool.Pool) *tooljournal.PGStore {
	return &tooljournal.PGStore{Pool: p, Local: false, Producer: true, Guard: func(ctx context.Context, tx pgx.Tx, sid int64) error {
		var open bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM training_sessions WHERE id=$1 AND status='running' AND deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM brain_sessions WHERE session_id=$1 AND phase IN ('closing','reviewing','completed','failed')))`, sid).Scan(&open)
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
		err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM training_sessions WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)`, sid, uid).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return oe.ErrNotFound
		}
		return nil
	}
}

func NewOperationStore(p *pgxpool.Pool) *tooljournal.PGStore {
	return &tooljournal.PGStore{Pool: p, Local: true}
}
