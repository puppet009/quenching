package service

import (
	"context"

	"github.com/quenching/user-service/internal/model"
)

// UserStore 用户仓储接口。生产用 PG 实现，单测用 fake。
type UserStore interface {
	// ExistsByAccount 按账号（手机号或邮箱）判断是否已注册。
	ExistsByAccount(ctx context.Context, t model.AccountType, account string) (bool, error)
	// Create 创建用户并初始化能力档案（同事务）。
	Create(ctx context.Context, u *model.User) (int64, error)
	// FindByAccount 按账号查用户；不存在返回 (nil,nil)。
	FindByAccount(ctx context.Context, t model.AccountType, account string) (*model.User, error)
	// FindByID 按ID查用户。
	FindByID(ctx context.Context, id int64) (*model.User, error)
	// UpdatePassword 更新用户密码哈希。
	UpdatePassword(ctx context.Context, userID int64, newPasswordHash string) error
}
