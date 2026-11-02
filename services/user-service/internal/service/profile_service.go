// 个人中心领域服务：当前用户账号 + 能力档案读取。
package service

import (
	"context"
	"errors"

	"github.com/quenching/user-service/internal/model"
	"github.com/quenching/user-service/internal/repository"
)

// ErrProfileNotFound 用户不存在（或已删除）。
var ErrProfileNotFound = errors.New("用户不存在")

type ProfileService struct {
	profiles *repository.ProfileRepo
}

func NewProfileService(profiles *repository.ProfileRepo) *ProfileService {
	return &ProfileService{profiles: profiles}
}

// GetMyProfile 返回当前用户账号与能力档案；被封禁用户返回 ErrUserBanned。
func (s *ProfileService) GetMyProfile(ctx context.Context, uid int64) (*model.User, *model.UserProfile, error) {
	u, p, err := s.profiles.GetByUserID(ctx, uid)
	if err != nil {
		return nil, nil, err
	}
	if u == nil {
		return nil, nil, ErrProfileNotFound
	}
	if u.IsBanned() {
		return nil, nil, ErrUserBanned
	}
	return u, p, nil
}

// AckRiskNotice 确认 AI 风险告知（幂等）；用户不存在或被封禁时拒绝。
func (s *ProfileService) AckRiskNotice(ctx context.Context, uid int64) error {
	u, _, err := s.profiles.GetByUserID(ctx, uid)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrProfileNotFound
	}
	if u.IsBanned() {
		return ErrUserBanned
	}
	return s.profiles.AckRiskNotice(ctx, uid)
}

// UpdateProfile 更新当前用户基本资料（昵称/头像/简介）；用户不存在或被封禁时拒绝。
// 字段长度与格式校验由 handler 负责，到达本层的值均已合法。
func (s *ProfileService) UpdateProfile(ctx context.Context, uid int64, nickname, avatarURL, bio string) error {
	u, _, err := s.profiles.GetByUserID(ctx, uid)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrProfileNotFound
	}
	if u.IsBanned() {
		return ErrUserBanned
	}
	return s.profiles.UpdateProfile(ctx, uid, nickname, avatarURL, bio)
}
