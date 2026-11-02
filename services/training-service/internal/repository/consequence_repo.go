// 踩坑次生故障登记的 PG 仓储（T3-12）。
//
// 编排层只依赖 service.ConsequenceStore；这里落 ai_consequence_injections，
// 以 pitfall_message_id 为幂等键（一条坑点消息至多恶化一次），并维护
// pending -> injected -> revoked / pending -> failed 状态机。
// 本地无 PG 时由 service 层内存 fake 单测覆盖。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/service"
)

// 编译期保证实现编排层要求的 service.ConsequenceStore。
var _ service.ConsequenceStore = (*ConsequenceRepo)(nil)

// ConsequenceRepo 次生故障登记 PG 仓储。
type ConsequenceRepo struct {
	pool *pgxpool.Pool
}

// NewConsequenceRepo 创建次生故障登记仓储。
func NewConsequenceRepo(pool *pgxpool.Pool) *ConsequenceRepo {
	return &ConsequenceRepo{pool: pool}
}

// consequenceColumns 全列，顺序须与 scanConsequence 一致。
const consequenceColumns = `id, session_id, conversation_id, pitfall_message_id,
	pitfall_config_id, kind, cpu_percent, mem_percent, disk_percent,
	delay_millis, loss_percent, duration_sec, status, experiment_uid,
	fail_reason, created_at, updated_at`

// RegisterPending 幂等登记待注入记录：首次插入返回 applied=true；
// 该坑点消息已登记（任意状态）时不改写，返回既有记录与 applied=false。
func (r *ConsequenceRepo) RegisterPending(
	ctx context.Context, rec service.ConsequenceRecord,
) (*service.ConsequenceRecord, bool, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ai_consequence_injections (
			session_id, conversation_id, pitfall_message_id, pitfall_config_id,
			kind, cpu_percent, mem_percent, disk_percent, delay_millis,
			loss_percent, duration_sec, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (pitfall_message_id) DO NOTHING
		RETURNING `+consequenceColumns,
		rec.SessionID, rec.ConversationID, rec.PitfallMessageID, rec.ConfigID,
		rec.Kind, rec.CPUPercent, rec.MemPercent, rec.DiskPercent,
		rec.DelayMillis, rec.LossPercent, rec.DurationSec, service.ConsequenceStatusPending,
	)
	saved, err := scanConsequence(row)
	if err == nil {
		return saved, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	// 冲突：取回既有登记（不二次恶化）。
	existing, err := r.getByPitfallMessage(ctx, rec.PitfallMessageID)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

// MarkInjected pending -> injected，回填故障引擎实验句柄。
func (r *ConsequenceRepo) MarkInjected(ctx context.Context, id int64, experimentUID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE ai_consequence_injections
		 SET status=$2, experiment_uid=$3, fail_reason=NULL, updated_at=now()
		 WHERE id=$1 AND status=$4`,
		id, service.ConsequenceStatusInjected, experimentUID, service.ConsequenceStatusPending)
	return err
}

// MarkFailed pending -> failed，记录失败原因（终态；仅观测用，不阻断训练）。
func (r *ConsequenceRepo) MarkFailed(ctx context.Context, id int64, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE ai_consequence_injections
		 SET status=$2, fail_reason=$3, updated_at=now()
		 WHERE id=$1 AND status=$4`,
		id, service.ConsequenceStatusFailed, reason, service.ConsequenceStatusPending)
	return err
}

// MarkRevoked injected -> revoked（撤销终态）。
func (r *ConsequenceRepo) MarkRevoked(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE ai_consequence_injections
		 SET status=$2, updated_at=now()
		 WHERE id=$1 AND status=$3`,
		id, service.ConsequenceStatusRevoked, service.ConsequenceStatusInjected)
	return err
}

// ListActiveBySession 列出会话仍 injected、结束时需要撤销的记录（按注入先后）。
func (r *ConsequenceRepo) ListActiveBySession(
	ctx context.Context, sessionID int64,
) ([]service.ConsequenceRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+consequenceColumns+`
		 FROM ai_consequence_injections
		 WHERE session_id=$1 AND status=$2
		 ORDER BY id`,
		sessionID, service.ConsequenceStatusInjected)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]service.ConsequenceRecord, 0)
	for rows.Next() {
		rec, err := scanConsequence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

// getByPitfallMessage 按坑点消息取既有登记。
func (r *ConsequenceRepo) getByPitfallMessage(
	ctx context.Context, pitfallMessageID int64,
) (*service.ConsequenceRecord, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+consequenceColumns+`
		 FROM ai_consequence_injections
		 WHERE pitfall_message_id=$1`, pitfallMessageID)
	return scanConsequence(row)
}

// rowScanner 兼容 QueryRow 与 Query 行。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanConsequence(row rowScanner) (*service.ConsequenceRecord, error) {
	var rec service.ConsequenceRecord
	var experimentUID, failReason *string
	err := row.Scan(
		&rec.ID, &rec.SessionID, &rec.ConversationID, &rec.PitfallMessageID,
		&rec.ConfigID, &rec.Kind, &rec.CPUPercent, &rec.MemPercent, &rec.DiskPercent,
		&rec.DelayMillis, &rec.LossPercent, &rec.DurationSec, &rec.Status,
		&experimentUID, &failReason, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if experimentUID != nil {
		rec.ExperimentUID = *experimentUID
	}
	if failReason != nil {
		rec.FailReason = *failReason
	}
	return &rec, nil
}
