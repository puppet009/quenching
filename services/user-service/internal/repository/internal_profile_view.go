// 服务间内部查询专用的用户协作档案最小投影与端口。
//
// training-service 不再直接读取或写入 user_profiles，统一经 user-service 的
// /internal/v1 接口访问。投影只暴露协作分组/等级/均分/雷达，不包含手机号、
// 邮箱、账号状态、技术雷达等账号或其他域字段。
package repository

import (
	"context"
	"encoding/json"
)

// InternalCollabProfileView 用户协作档案最小投影。
type InternalCollabProfileView struct {
	UserID         int64           `json:"user_id"`
	CollabGroup    string          `json:"collab_group"`
	CollabLevel    string          `json:"collab_level"`
	AvgCollabScore *float64        `json:"avg_collab_score"`
	CollabRadar    json.RawMessage `json:"collab_radar"`
}

// InternalProfileStore 用户协作档案的服务间读写端口。
type InternalProfileStore interface {
	// GetCollabProfile 读取用户协作档案；用户或档案不存在时返回 (nil, nil)。
	GetCollabProfile(ctx context.Context, userID int64) (*InternalCollabProfileView, error)
	// AssignCollabGroupIfAbsent 仅在分组为空时条件写入；返回最终分组及是否本次写入。
	AssignCollabGroupIfAbsent(
		ctx context.Context, userID int64, group string,
	) (finalGroup string, applied bool, err error)
	// UpdateCollabRollup 更新协作雷达、历史均分和等级；found=false 表示档案不存在。
	UpdateCollabRollup(
		ctx context.Context, userID int64,
		radar json.RawMessage, avgScore float64, level string,
	) (found bool, err error)
}
