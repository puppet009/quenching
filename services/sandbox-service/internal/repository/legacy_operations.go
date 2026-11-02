package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	oe "github.com/quenching/proto/operationevidence"
	"strings"
)

type LegacyOperationRepo struct{ pool *pgxpool.Pool }

func NewLegacyOperationRepo(pool *pgxpool.Pool) *LegacyOperationRepo {
	return &LegacyOperationRepo{pool}
}
func (r *LegacyOperationRepo) Record(ctx context.Context, b oe.LegacyBatch) (oe.LegacyReceipt, error) {
	receipt := oe.LegacyReceipt{RequestID: b.RequestID, SessionID: b.SessionID, UserID: b.UserID}
	if b.RequestID == "" || b.SessionID <= 0 || b.UserID <= 0 || len(b.Operations) == 0 || len(b.Operations) > 500 {
		return receipt, errors.New("invalid batch")
	}
	for _, op := range b.Operations {
		if strings.TrimSpace(op.Type) == "" {
			return receipt, errors.New("operation type required")
		}
		raw, e := json.Marshal(op.Detail)
		if e != nil || len(raw) > 7<<20 {
			return receipt, errors.New("operation detail exceeds projection budget")
		}
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return receipt, e
	}
	defer tx.Rollback(ctx)
	hash := oe.RequestHash(b)
	tag, e := tx.Exec(ctx, `INSERT INTO sandbox_operation_reports(request_id,request_hash) VALUES($1,$2) ON CONFLICT DO NOTHING`, b.RequestID, hash)
	if e != nil {
		return receipt, e
	}
	var previous string
	var raw []byte
	if e = tx.QueryRow(ctx, `SELECT request_hash,receipt FROM sandbox_operation_reports WHERE request_id=$1 FOR UPDATE`, b.RequestID).Scan(&previous, &raw); e != nil {
		return receipt, e
	}
	if previous != hash {
		return receipt, oe.ErrConflict
	}
	if tag.RowsAffected() == 0 {
		if len(raw) == 0 {
			return receipt, errors.New("receipt incomplete")
		}
		e = json.Unmarshal(raw, &receipt)
		return receipt, e
	}
	for _, op := range b.Operations {
		var detail []byte
		if op.Detail != nil {
			detail, e = json.Marshal(op.Detail)
			if e != nil {
				return receipt, e
			}
		}
		result := op.Result
		if result == "" {
			result = "executed"
		}
		var vts any
		if op.VirtualTimestamp >= 0 {
			vts = op.VirtualTimestamp
		}
		_, e = tx.Exec(ctx, `INSERT INTO sandbox_session_operations(session_id,operation_type,operation_category,target,detail,result,penalty,virtual_timestamp,tenant_id) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,$7,$8,0)`, b.SessionID, op.Type, op.Category, op.Target, detail, result, op.Penalty, vts)
		if e != nil {
			return receipt, e
		}
		receipt.Written++
	}
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM sandbox_session_operations WHERE session_id=$1 AND deleted_at IS NULL`, b.SessionID).Scan(&receipt.Total); e != nil {
		return receipt, e
	}
	raw, e = json.Marshal(receipt)
	if e != nil {
		return receipt, e
	}
	if _, e = tx.Exec(ctx, `UPDATE sandbox_operation_reports SET receipt=$2 WHERE request_id=$1`, b.RequestID, raw); e != nil {
		return receipt, e
	}
	return receipt, tx.Commit(ctx)
}
func (r *LegacyOperationRepo) Pending(ctx context.Context) ([]oe.LegacyRow, error) {
	rows, e := r.pool.Query(ctx, `SELECT id,session_id,operation_type,coalesce(operation_category,''),coalesce(target,''),detail,coalesce(result,''),penalty::float8,virtual_timestamp,tenant_id,created_at,deleted_at FROM sandbox_session_operations WHERE NOT projection_delivered ORDER BY id LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []oe.LegacyRow{}
	for rows.Next() {
		var v oe.LegacyRow
		if e = rows.Scan(&v.ID, &v.SessionID, &v.Type, &v.Category, &v.Target, &v.Detail, &v.Result, &v.Penalty, &v.VirtualTimestamp, &v.TenantID, &v.CreatedAt, &v.DeletedAt); e != nil {
			return nil, e
		}
		v.CreatedAt = v.CreatedAt.UTC()
		if v.DeletedAt != nil {
			utc := v.DeletedAt.UTC()
			v.DeletedAt = &utc
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *LegacyOperationRepo) Delivered(ctx context.Context, ids []int64) error {
	_, e := r.pool.Exec(ctx, `UPDATE sandbox_session_operations SET projection_delivered=true WHERE id=ANY($1)`, ids)
	return e
}

func (r *LegacyOperationRepo) Checkpoint(ctx context.Context, sid int64) (oe.LegacyCheckpoint, error) {
	c := oe.LegacyCheckpoint{SessionID: sid}
	e := r.pool.QueryRow(ctx, `SELECT COALESCE(max(id),0),count(*) FILTER(WHERE NOT projection_delivered) FROM sandbox_session_operations WHERE session_id=$1`, sid).Scan(&c.Watermark, &c.Pending)
	return c, e
}
