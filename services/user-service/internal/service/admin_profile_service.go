// 管理员身份服务：返回登录用户的角色与权限，并对封禁账号做拦截。
package service

import (
	"context"
	"errors"

	"github.com/quenching/user-service/internal/model"
	"github.com/quenching/user-service/internal/repository"
)

var ErrAdminProfileNotFound = errors.New("管理员身份信息不存在")

type AdminProfileService struct {
	repo *repository.AdminProfileRepo
}

func NewAdminProfileService(repo *repository.AdminProfileRepo) *AdminProfileService {
	return &AdminProfileService{repo: repo}
}

// GetByUserID 取指定用户的管理员身份信息；用户不存在或被封禁时返回错误。
func (s *AdminProfileService) GetByUserID(ctx context.Context, uid int64) (*model.AdminProfile, error) {
	p, err := s.repo.GetByUserID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrAdminProfileNotFound
	}
	if p.Status == model.StatusBanned {
		return nil, ErrUserBanned
	}
	return p, nil
}
