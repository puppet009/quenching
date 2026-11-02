package service

import (
	"context"
	"fmt"

	"github.com/quenching/sandbox-service/internal/model"
)

// QuotaService 资源配额校验服务。
// 负责在创建沙箱前检查用户并发数、集群资源等限制，超限直接拒绝，避免浪费集群资源。
type QuotaService struct {
	db Store
}

func NewQuotaService(db Store) *QuotaService {
	return &QuotaService{db: db}
}

// CheckCreateQuota 校验创建沙箱的配额。
// 通过返回 nil，不通过返回具体的业务错误（*response.Error）。
//
// 当前校验项：
//   - 用户并发沙箱数 ≤ MaxSandboxPerUser（默认 1）
//
// 后续可扩展：集群总资源水位检查、节点资源预检等。
func (s *QuotaService) CheckCreateQuota(ctx context.Context, userID int64) error {
	count, err := s.db.CountActiveByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("统计用户活跃沙箱数失败: %w", err)
	}
	if count >= model.MaxSandboxPerUser {
		return quotaExceededErr(count, model.MaxSandboxPerUser)
	}
	return nil
}
