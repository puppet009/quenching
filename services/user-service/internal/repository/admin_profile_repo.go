// 管理员身份仓储：按用户ID联查 user_roles/roles，聚合角色名与权限并集。
package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/user-service/internal/model"
)

type AdminProfileRepo struct {
	pool *pgxpool.Pool
}

func NewAdminProfileRepo(pool *pgxpool.Pool) *AdminProfileRepo {
	return &AdminProfileRepo{pool: pool}
}

// GetByUserID 返回用户的管理员身份信息；用户不存在时返回 (nil, nil)。
// 角色名与权限均跨角色聚合，权限取各角色 permissions JSONB 数组的并集（去重）。
func (r *AdminProfileRepo) GetByUserID(ctx context.Context, uid int64) (*model.AdminProfile, error) {
	p := &model.AdminProfile{}
	var permissions json.RawMessage
	err := r.pool.QueryRow(ctx, `
		SELECT u.id, u.account_type, COALESCE(u.phone,''), COALESCE(u.email,''),
		       u.nickname, COALESCE(u.avatar_url,''), u.status,
		       COALESCE((
		           SELECT array_agg(r.name ORDER BY r.name)
		             FROM user_roles ur
		             JOIN roles r ON r.id = ur.role_id
		            WHERE ur.user_id = u.id
		              AND ur.deleted_at IS NULL AND r.deleted_at IS NULL
		       ), ARRAY[]::text[]),
		       COALESCE((
		           SELECT jsonb_agg(DISTINCT elem)
		             FROM user_roles ur2
		             JOIN roles r2 ON r2.id = ur2.role_id,
		                  LATERAL jsonb_array_elements(COALESCE(r2.permissions, '[]'::jsonb)) AS elem
		            WHERE ur2.user_id = u.id
		              AND ur2.deleted_at IS NULL AND r2.deleted_at IS NULL
		       ), '[]'::jsonb)
		  FROM users u
		 WHERE u.id = $1 AND u.deleted_at IS NULL`, uid).Scan(
		&p.ID, &p.AccountType, &p.Phone, &p.Email,
		&p.Nickname, &p.AvatarURL, &p.Status,
		&p.Roles, &permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Roles = defaultStringSlice(p.Roles)
	p.Permissions = decodePermissions(permissions)
	return p, nil
}

func decodePermissions(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var perms []string
	if err := json.Unmarshal(raw, &perms); err != nil {
		return []string{}
	}
	return perms
}

func defaultStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
