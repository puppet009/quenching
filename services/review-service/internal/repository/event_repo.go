// Package repository 分析数据仓储层。
package repository

import (
	"context"
	"time"

	"github.com/quenching/review-service/internal/model"
)

// EventRepository 事件仓储接口。
type EventRepository interface {
	// Track 记录一个事件。
	// 对于唯一事件（如 register/first_start），如果用户已记录过则返回 (false, nil)。
	Track(ctx context.Context, event *model.Event) (bool, error)

	// HasUserEvent 检查用户是否已触发过某事件。
	HasUserEvent(ctx context.Context, userID int64, eventType string) (bool, error)

	// CountByEventType 统计某事件的触发次数（或触发用户数，unique=true 时按用户去重）。
	CountByEventType(ctx context.Context, eventType string, unique bool, start, end time.Time) (int, error)

	// GetUniqueUsers 获取在指定时间范围内触发过某事件的用户 ID 集合。
	GetUniqueUsers(ctx context.Context, eventType string, start, end time.Time) (map[int64]bool, error)

	// ListEvents 分页查询事件列表（调试/审计用）。
	ListEvents(ctx context.Context, userID int64, eventType string, page, pageSize int) ([]model.Event, int, error)

	// GetAverageNumericProperty 获取某事件类型指定数值属性的平均值。
	// propertyPath 支持简单的单层 key（如 "duration_seconds"、"score"）。
	GetAverageNumericProperty(ctx context.Context, eventType string, propertyKey string, start, end time.Time) (float64, error)

	// CountByPropertyValue 统计某事件中属性等于特定值的次数。
	// 用于筛选类统计，如统计评分 >= 4 的复盘评价数。
	CountByPropertyValue(ctx context.Context, eventType string, propertyKey string, compare string, value float64, start, end time.Time) (int, error)
}
