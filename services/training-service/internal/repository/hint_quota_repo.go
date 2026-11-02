// T3-5 渐进式提示配额的 PG 仓储。
//
// 配额口径：按训练会话、跨该会话下全部对话，从 ai_messages.is_hint 实时计数，
// 不在 training_sessions 上加计数列（与坑点 InjectedCount 的重建范式一致），
// 服务重启、多副本部署都不会丢次数或重复计数。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

var _ service.HintQuotaProvider = (*HintQuotaRepo)(nil)

// HintQuotaRepo 渐进式提示配额/用量只读仓储。
type HintQuotaRepo struct {
	pool *pgxpool.Pool
}

// NewHintQuotaRepo 创建提示配额仓储。
func NewHintQuotaRepo(pool *pgxpool.Pool) *HintQuotaRepo {
	return &HintQuotaRepo{pool: pool}
}

// Difficulty 返回会话难度的归一化取值（easy/normal/hard）。
//
// 历史数据/建表默认值是 'medium'（000003 迁移），这里归一为编排层使用的 'normal'；
// 会话行缺失或难度为空时回退 normal（normal 配额并非最宽松，回退不会放大泄露面）。
func (r *HintQuotaRepo) Difficulty(ctx context.Context, sessionID int64) (string, error) {
	var level *string
	err := r.pool.QueryRow(ctx,
		`SELECT difficulty FROM training_sessions WHERE id=$1 AND deleted_at IS NULL`,
		sessionID).Scan(&level)
	switch {
	case err == nil:
		if level == nil || *level == "" {
			return string(model.LevelNormal), nil
		}
		if *level == "medium" {
			return string(model.LevelNormal), nil
		}
		return *level, nil
	case errors.Is(err, pgx.ErrNoRows):
		return string(model.LevelNormal), nil
	default:
		return "", err
	}
}

// Usage 返回该会话已消耗的提示次数与已用最高级别（跨该会话全部对话）。
func (r *HintQuotaRepo) Usage(ctx context.Context, sessionID int64) (service.HintUsage, error) {
	var u service.HintUsage
	err := r.pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(MAX(m.hint_level), 0)
		 FROM ai_messages m
		 JOIN ai_conversations c ON c.id = m.conversation_id
		 WHERE c.session_id=$1 AND m.is_hint=true`,
		sessionID).Scan(&u.Used, &u.MaxLevel)
	if err != nil {
		return service.HintUsage{}, err
	}
	return u, nil
}
