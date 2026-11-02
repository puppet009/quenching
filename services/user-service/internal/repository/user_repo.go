// Package repository 基于 PostgreSQL 的用户仓储实现。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/user-service/internal/model"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) ExistsByAccount(ctx context.Context, t model.AccountType, account string) (bool, error) {
	col := "phone"
	if t == model.AccountEmail {
		col = "email"
	}
	var exists bool
	// #nosec G201 -- col 来自内部枚举，非用户输入
	q := "SELECT EXISTS(SELECT 1 FROM users WHERE " + col + "=$1)"
	if err := r.pool.QueryRow(ctx, q, account).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *UserRepo) Create(ctx context.Context, u *model.User) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	if u.AccountType == string(model.AccountPhone) {
		err = tx.QueryRow(ctx,
			`INSERT INTO users (account_type, phone, password_hash, nickname, avatar_url, status)
			 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			u.AccountType, u.Phone, u.PasswordHash, u.Nickname, u.AvatarURL, u.Status).Scan(&id)
	} else {
		err = tx.QueryRow(ctx,
			`INSERT INTO users (account_type, email, password_hash, nickname, avatar_url, status)
			 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			u.AccountType, u.Email, u.PasswordHash, u.Nickname, u.AvatarURL, u.Status).Scan(&id)
	}
	if err != nil {
		return 0, err
	}

	// 同事务初始化能力档案（collab_level 默认 C1，collab_group 留空待首训分组）
	if _, err = tx.Exec(ctx,
		`INSERT INTO user_profiles (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

func (r *UserRepo) FindByAccount(ctx context.Context, t model.AccountType, account string) (*model.User, error) {
	col := "phone"
	if t == model.AccountEmail {
		col = "email"
	}
	u := &model.User{}
	// #nosec G201 -- col 来自内部枚举
	q := `SELECT id, account_type, COALESCE(phone,''), COALESCE(email,''),
		password_hash, nickname, COALESCE(avatar_url,''), status, created_at
		FROM users WHERE ` + col + `=$1`
	err := r.pool.QueryRow(ctx, q, account).Scan(
		&u.ID, &u.AccountType, &u.Phone, &u.Email,
		&u.PasswordHash, &u.Nickname, &u.AvatarURL, &u.Status, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *UserRepo) FindByID(ctx context.Context, id int64) (*model.User, error) {
	u := &model.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, account_type, COALESCE(phone,''), COALESCE(email,''),
		password_hash, nickname, COALESCE(avatar_url,''), status, created_at
		FROM users WHERE id=$1`, id).Scan(
		&u.ID, &u.AccountType, &u.Phone, &u.Email,
		&u.PasswordHash, &u.Nickname, &u.AvatarURL, &u.Status, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *UserRepo) UpdatePassword(ctx context.Context, userID int64, newPasswordHash string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET password_hash=$1 WHERE id=$2`, newPasswordHash, userID)
	return err
}
