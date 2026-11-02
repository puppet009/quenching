// InternalProfileStore 的 PostgreSQL 实现：只读写协作档案白名单列。
package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 编译期断言：internalProfileRepo 满足 InternalProfileStore。
var _ InternalProfileStore = (*internalProfileRepo)(nil)

type internalProfileRepo struct {
	pool *pgxpool.Pool
}

// NewInternalProfileRepo 创建用户协作档案内部读写实现。
func NewInternalProfileRepo(pool *pgxpool.Pool) InternalProfileStore {
	return &internalProfileRepo{pool: pool}
}

// GetCollabProfile 读取未删除用户的未删除协作档案；不存在返回 (nil, nil)。
func (r *internalProfileRepo) GetCollabProfile(
	ctx context.Context, userID int64,
) (*InternalCollabProfileView, error) {
	var v InternalCollabProfileView
	err := r.pool.QueryRow(ctx, `
		SELECT p.user_id,
		       COALESCE(p.collab_group, ''),
		       COALESCE(p.collab_level, 'C1'),
		       p.avg_collab_score,
		       COALESCE(p.collab_radar, 'null'::jsonb)
		  FROM user_profiles p
		  JOIN users u ON u.id = p.user_id
		 WHERE p.user_id = $1
		   AND p.deleted_at IS NULL
		   AND u.deleted_at IS NULL
	`, userID).Scan(
		&v.UserID, &v.CollabGroup, &v.CollabLevel,
		&v.AvgCollabScore, &v.CollabRadar,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// AssignCollabGroupIfAbsent 仅在分组为空时条件写入，避免并发首训互相覆盖。
func (r *internalProfileRepo) AssignCollabGroupIfAbsent(
	ctx context.Context, userID int64, group string,
) (string, bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_profiles p
		   SET collab_group = $2,
		       updated_at = now()
		 WHERE p.user_id = $1
		   AND p.collab_group IS NULL
		   AND p.deleted_at IS NULL
		   AND EXISTS (
		       SELECT 1 FROM users u
		       WHERE u.id = p.user_id AND u.deleted_at IS NULL
		   )
	`, userID, group)
	if err != nil {
		return "", false, err
	}
	if tag.RowsAffected() == 1 {
		return group, true, nil
	}

	existing, err := r.GetCollabProfile(ctx, userID)
	if err != nil {
		return "", false, err
	}
	if existing == nil {
		return "", false, nil
	}
	return existing.CollabGroup, false, nil
}

// UpdateCollabRollup 更新协作滚动汇总；用户或档案不存在时 found=false。
func (r *internalProfileRepo) UpdateCollabRollup(
	ctx context.Context, userID int64,
	radar json.RawMessage, avgScore float64, level string,
) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_profiles p
		   SET collab_radar = $2,
		       avg_collab_score = $3,
		       collab_level = $4,
		       updated_at = now()
		 WHERE p.user_id = $1
		   AND p.deleted_at IS NULL
		   AND EXISTS (
		       SELECT 1 FROM users u
		       WHERE u.id = p.user_id AND u.deleted_at IS NULL
		   )
	`, userID, radar, avgScore, level)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
