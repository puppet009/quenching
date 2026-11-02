package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"time"
)

const allocationLock int64 = 1009202601

func (s *PGStore) WithAllocationLock(ctx context.Context, fn func(context.Context) error) error {
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, allocationLock).Scan(&locked); e != nil {
		return e
	}
	if !locked {
		return nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, e := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, allocationLock); e != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()
	return fn(ctx)
}
func (s *PGStore) CreateQueued(ctx context.Context, sb *model.Sandbox) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, sb.UserID); e != nil {
		return e
	}
	existing, e := scanSandbox(tx.QueryRow(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE session_id=$1 ORDER BY id DESC LIMIT 1`, sb.SessionID))
	if e == nil {
		if e = service.ValidateQueuedReuse(existing, *sb, time.Now()); e != nil {
			return e
		}
		var deleted bool
		if e = tx.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM sandboxes WHERE id=$1`, existing.ID).Scan(&deleted); e != nil {
			return e
		}
		if deleted {
			return service.ErrSandboxConflict.Unwrap()
		}
		*sb = existing
		return tx.Commit(ctx)
	}
	if e != pgx.ErrNoRows {
		return e
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM sandboxes WHERE user_id=$1 AND `+activeWhere, sb.UserID).Scan(&count); e != nil {
		return e
	}
	if count >= model.MaxSandboxPerUser {
		return service.ErrSandboxQuotaExceeded.Unwrap()
	}
	raw, e := json.Marshal(sb.ResourcePlan)
	if e != nil {
		return e
	}
	e = tx.QueryRow(ctx, `INSERT INTO sandboxes(uuid,project_id,user_id,session_id,namespace,status,template_slug,template_digest,resource_plan) VALUES($1,$2,$3,$4,$5,'queued',$6,$7,$8) RETURNING id,created_at`, uuid.NewString(), sb.ProjectID, sb.UserID, sb.SessionID, sb.Namespace, sb.TemplateSlug, sb.TemplateDigest, raw).Scan(&sb.ID, &sb.CreatedAt)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *PGStore) AllocationCandidates(ctx context.Context) ([]model.Sandbox, error) {
	rows, e := s.pool.Query(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE status IN ('queued','deploying') AND template_slug<>'' AND completed_at IS NULL AND deleted_at IS NULL ORDER BY created_at,id LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return collectSandboxes(rows)
}
func (s *PGStore) ListReservations(ctx context.Context) ([]model.Sandbox, error) {
	rows, e := s.pool.Query(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE resources_admitted AND `+activeWhere)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return collectSandboxes(rows)
}
func (s *PGStore) Admit(ctx context.Context, sid int64) (bool, error) {
	tag, e := s.pool.Exec(ctx, `UPDATE sandboxes SET status='deploying',resources_admitted=true,allocation_reason='' WHERE session_id=$1 AND status='queued' AND completed_at IS NULL AND deleted_at IS NULL AND COALESCE(last_heartbeat_at,created_at)>now()-interval '30 minutes'`, sid)
	return tag.RowsAffected() == 1, e
}
func (s *PGStore) SetAllocationReason(ctx context.Context, sid int64, reason string) error {
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	_, e := s.pool.Exec(ctx, `UPDATE sandboxes SET allocation_reason=$2 WHERE session_id=$1 AND status IN ('queued','deploying')`, sid, reason)
	return e
}
func (s *PGStore) MarkAllocationReady(ctx context.Context, sid int64) error {
	tag, e := s.pool.Exec(ctx, `UPDATE sandboxes SET status='ready' WHERE session_id=$1 AND status='deploying' AND completed_at IS NULL AND deleted_at IS NULL`, sid)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("allocation already closed")
	}
	return nil
}
