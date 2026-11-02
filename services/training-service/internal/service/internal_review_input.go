// 服务间复盘聚合查询服务（架构拆库第五批）。
//
// 对 review-service 暴露会话复盘主输入与 AI 坑点原始数据，review-service
// 不再直接查本服务数据库。识别判定与识别率统计属于复盘领域，仍保留在
// review-service 侧，本服务只做取数与聚合。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// 内部查询哨兵错误，供 handler 映射 HTTP 状态。
var (
	ErrInternalReviewSessionNotFound = errors.New("training session not found")
)

// InternalReviewInputService 复盘聚合内部查询服务。
type InternalReviewInputService struct {
	store ReviewInputStore
	brain interface {
		ReviewBrainInput(context.Context, int64) (json.RawMessage, error)
	}
}

// NewInternalReviewInputService 创建复盘聚合查询服务。
func NewInternalReviewInputService(store ReviewInputStore) *InternalReviewInputService {
	return &InternalReviewInputService{store: store}
}

// GetReviewInput 返回复盘主输入；会话不存在返回 ErrInternalReviewSessionNotFound。
func (s *InternalReviewInputService) GetReviewInput(
	ctx context.Context, sessionID int64,
) (*SessionReviewInput, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}
	in, err := s.store.GetReviewInput(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, fmt.Errorf("training session %d: %w", sessionID, ErrInternalReviewSessionNotFound)
	}
	if s.brain != nil {
		in.Brain, err = s.brain.ReviewBrainInput(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	}
	return in, nil
}

// ListPitfalls 返回会话 AI 坑点原始数据（无坑点为空切片）。
func (s *InternalReviewInputService) ListPitfalls(
	ctx context.Context, sessionID int64,
) ([]ReviewPitfallRawItem, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}
	return s.store.ListPitfalls(ctx, sessionID)
}

// ListChanges 返回助手文件修改记录（无记录为空切片）。
func (s *InternalReviewInputService) ListChanges(
	ctx context.Context, sessionID int64,
) ([]ReviewChangeItem, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}
	return s.store.ListChanges(ctx, sessionID)
}

func (s *InternalReviewInputService) SetBrainInput(b interface {
	ReviewBrainInput(context.Context, int64) (json.RawMessage, error)
}) {
	s.brain = b
}
