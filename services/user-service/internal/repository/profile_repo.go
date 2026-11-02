// 能力档案仓储：users 与 user_profiles 联查（1:1，注册时同事务初始化，
// 仍用 LEFT JOIN 兜底历史数据）。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/user-service/internal/model"
)

type ProfileRepo struct {
	pool *pgxpool.Pool
}

func NewProfileRepo(pool *pgxpool.Pool) *ProfileRepo {
	return &ProfileRepo{pool: pool}
}

// GetByUserID 返回用户账号与其能力档案；用户不存在时返回 (nil, nil, nil)。
func (r *ProfileRepo) GetByUserID(ctx context.Context, uid int64) (*model.User, *model.UserProfile, error) {
	u := &model.User{}
	p := &model.UserProfile{}
	err := r.pool.QueryRow(ctx,
		`SELECT u.id, u.account_type, COALESCE(u.phone,''), COALESCE(u.email,''),
		        u.nickname, COALESCE(u.avatar_url,''), COALESCE(u.bio,''), u.status, u.created_at,
		        COALESCE(p.collab_level, 'C1'), COALESCE(p.collab_group,''),
		        p.avg_collab_score, p.avg_score,
		        COALESCE(p.collab_radar, 'null'::jsonb),
		        COALESCE(p.skill_radar, 'null'::jsonb)
		 FROM users u
		 LEFT JOIN user_profiles p ON p.user_id = u.id
		 WHERE u.id = $1 AND u.deleted_at IS NULL`, uid).Scan(
		&u.ID, &u.AccountType, &u.Phone, &u.Email,
		&u.Nickname, &u.AvatarURL, &u.Bio, &u.Status, &u.CreatedAt,
		&p.CollabLevel, &p.CollabGroup,
		&p.AvgCollabScore, &p.AvgScore,
		&p.CollabRadar, &p.SkillRadar)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	p.UserID = u.ID
	return u, p, nil
}

// AckRiskNotice 幂等确认 AI 风险告知。user_profiles 行在注册时初始化，
// 用 UPDATE 不判断行数：重复确认同样视为成功。
func (r *ProfileRepo) AckRiskNotice(ctx context.Context, uid int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE user_profiles SET risk_notice_ack = true, updated_at = now()
		  WHERE user_id = $1`, uid)
	return err
}

// UpdateProfile 更新当前用户的基本展示资料（昵称/头像/简介）。
// 仅更新 users 表三个可编辑字段；用户不存在（已删除）时影响 0 行。
func (r *ProfileRepo) UpdateProfile(ctx context.Context, uid int64, nickname, avatarURL, bio string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users
		    SET nickname = $2, avatar_url = $3, bio = $4, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL`,
		uid, nickname, avatarURL, bio)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
