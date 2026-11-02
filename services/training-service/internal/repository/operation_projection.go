package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	oe "github.com/quenching/proto/operationevidence"
)

type OperationProjectionRepo struct{ pool *pgxpool.Pool }

func NewOperationProjectionRepo(pool *pgxpool.Pool) *OperationProjectionRepo {
	return &OperationProjectionRepo{pool}
}
func (r *OperationProjectionRepo) Owner(ctx context.Context, sid, uid int64) (bool, error) {
	var owned bool
	if sid <= 0 || uid < 0 {
		return false, nil
	}
	e := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM training_sessions WHERE id=$1 AND ($2=0 OR (user_id=$2 AND deleted_at IS NULL)))`, sid, uid).Scan(&owned)
	return owned, e
}
func (r *OperationProjectionRepo) IngestLegacy(ctx context.Context, b oe.LegacyProjection) error {
	if len(b.Rows) < 1 || len(b.Rows) > 100 {
		return errors.New("invalid projection batch")
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for _, row := range b.Rows {
		if row.ID <= 0 || row.SessionID <= 0 || row.CreatedAt.IsZero() {
			return errors.New("invalid source row")
		}
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM training_sessions WHERE id=$1)`, row.SessionID).Scan(&exists); e != nil || !exists {
			return errors.New("unknown source session")
		}
		var old oe.LegacyRow
		e = tx.QueryRow(ctx, `SELECT id,session_id,operation_type,coalesce(operation_category,''),coalesce(target,''),detail,coalesce(result,''),penalty::float8,virtual_timestamp,tenant_id,created_at,deleted_at FROM training_operation_projection WHERE id=$1 FOR UPDATE`, row.ID).Scan(&old.ID, &old.SessionID, &old.Type, &old.Category, &old.Target, &old.Detail, &old.Result, &old.Penalty, &old.VirtualTimestamp, &old.TenantID, &old.CreatedAt, &old.DeletedAt)
		if e == nil {
			if e = validateLegacyReplay(old, row); e != nil {
				return e
			}
			continue
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if row.Type == "" || row.ReplayHash != "" {
			return errors.New("new source row requires full valid evidence")
		}
		if len(row.Detail) > 0 && !json.Valid(row.Detail) {
			return fmt.Errorf("invalid legacy detail")
		}
		_, e = tx.Exec(ctx, `INSERT INTO training_operation_projection(id,session_id,operation_type,operation_category,target,detail,result,penalty,virtual_timestamp,tenant_id,created_at,deleted_at) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12)`, row.ID, row.SessionID, row.Type, row.Category, row.Target, row.Detail, row.Result, row.Penalty, row.VirtualTimestamp, row.TenantID, row.CreatedAt, row.DeletedAt)
		if e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

func normalizedLegacyRow(row oe.LegacyRow) oe.LegacyRow {
	row.CreatedAt = row.CreatedAt.UTC()
	if row.DeletedAt != nil {
		utc := row.DeletedAt.UTC()
		row.DeletedAt = &utc
	}
	return row
}
func validateLegacyReplay(old, row oe.LegacyRow) error {
	old = normalizedLegacyRow(old)
	row = normalizedLegacyRow(row)
	if old.ID != row.ID || old.SessionID != row.SessionID {
		return oe.ErrConflict
	}
	if row.ReplayHash != "" {
		if oe.RequestHash(old) != row.ReplayHash {
			return oe.ErrConflict
		}
		return nil
	}
	if oe.RequestHash(old) != oe.RequestHash(row) {
		return oe.ErrConflict
	}
	return nil
}
