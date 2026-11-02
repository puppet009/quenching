package repository

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/quenching/review-service/internal/model"
)

// InMemoryEventRepo 内存版事件仓储（开发/测试用）。
// 线程安全。
type InMemoryEventRepo struct {
	mu     sync.RWMutex
	events []model.Event
	nextID int64

	// 唯一事件索引：userID -> eventType -> true
	uniqueIndex map[int64]map[string]bool
}

// NewInMemoryEventRepo 创建内存事件仓储。
func NewInMemoryEventRepo() *InMemoryEventRepo {
	return &InMemoryEventRepo{
		events:      make([]model.Event, 0),
		uniqueIndex: make(map[int64]map[string]bool),
		nextID:      1,
	}
}

func (r *InMemoryEventRepo) Track(_ context.Context, event *model.Event) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = now
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}

	// 唯一事件去重
	if model.IsUniqueEvent(event.EventType) && event.UserID > 0 {
		if r.hasUniqueEventLocked(event.UserID, event.EventType) {
			return false, nil // 已记录过，不再重复
		}
		r.markUniqueLocked(event.UserID, event.EventType)
	}

	event.ID = r.nextID
	r.nextID++

	r.events = append(r.events, *event)
	return true, nil
}

func (r *InMemoryEventRepo) HasUserEvent(_ context.Context, userID int64, eventType string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.hasUniqueEventLocked(userID, eventType), nil
}

func (r *InMemoryEventRepo) CountByEventType(_ context.Context, eventType string, unique bool, start, end time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if unique {
		// 按用户去重计数
		users := make(map[int64]bool)
		for _, e := range r.events {
			if e.EventType != eventType {
				continue
			}
			if !start.IsZero() && e.OccurredAt.Before(start) {
				continue
			}
			if !end.IsZero() && e.OccurredAt.After(end) {
				continue
			}
			if e.UserID > 0 {
				users[e.UserID] = true
			}
		}
		return len(users), nil
	}

	// 总次数
	count := 0
	for _, e := range r.events {
		if e.EventType != eventType {
			continue
		}
		if !start.IsZero() && e.OccurredAt.Before(start) {
			continue
		}
		if !end.IsZero() && e.OccurredAt.After(end) {
			continue
		}
		count++
	}
	return count, nil
}

func (r *InMemoryEventRepo) GetUniqueUsers(_ context.Context, eventType string, start, end time.Time) (map[int64]bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	users := make(map[int64]bool)
	for _, e := range r.events {
		if e.EventType != eventType {
			continue
		}
		if !start.IsZero() && e.OccurredAt.Before(start) {
			continue
		}
		if !end.IsZero() && e.OccurredAt.After(end) {
			continue
		}
		if e.UserID > 0 {
			users[e.UserID] = true
		}
	}
	return users, nil
}

func (r *InMemoryEventRepo) ListEvents(_ context.Context, userID int64, eventType string, page, pageSize int) ([]model.Event, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []model.Event
	for i := len(r.events) - 1; i >= 0; i-- { // 倒序（最新在前）
		e := r.events[i]
		if userID > 0 && e.UserID != userID {
			continue
		}
		if eventType != "" && e.EventType != eventType {
			continue
		}
		filtered = append(filtered, e)
	}

	total := len(filtered)

	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= total {
		return []model.Event{}, total, nil
	}
	if end > total {
		end = total
	}

	// 按时间倒序
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].OccurredAt.After(filtered[j].OccurredAt)
	})

	return filtered[start:end], total, nil
}

// GetAverageNumericProperty 获取某事件指定数值属性的平均值。
func (r *InMemoryEventRepo) GetAverageNumericProperty(_ context.Context, eventType string, propertyKey string, start, end time.Time) (float64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sum float64
	count := 0

	for _, e := range r.events {
		if e.EventType != eventType {
			continue
		}
		if !start.IsZero() && e.OccurredAt.Before(start) {
			continue
		}
		if !end.IsZero() && e.OccurredAt.After(end) {
			continue
		}
		if e.Properties == nil {
			continue
		}

		val, ok := e.Properties[propertyKey]
		if !ok {
			continue
		}

		// 支持 float64 / int / int64
		switch v := val.(type) {
		case float64:
			sum += v
			count++
		case int:
			sum += float64(v)
			count++
		case int64:
			sum += float64(v)
			count++
		}
	}

	if count == 0 {
		return 0, nil
	}
	return sum / float64(count), nil
}

// CountByPropertyValue 按属性值筛选统计事件数。
// compare 支持：eq / ne / gt / gte / lt / lte
func (r *InMemoryEventRepo) CountByPropertyValue(_ context.Context, eventType string, propertyKey string, compare string, value float64, start, end time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0

	for _, e := range r.events {
		if e.EventType != eventType {
			continue
		}
		if !start.IsZero() && e.OccurredAt.Before(start) {
			continue
		}
		if !end.IsZero() && e.OccurredAt.After(end) {
			continue
		}
		if e.Properties == nil {
			continue
		}

		val, ok := e.Properties[propertyKey]
		if !ok {
			continue
		}

		// 转成 float64 比较
		var fv float64
		switch v := val.(type) {
		case float64:
			fv = v
		case int:
			fv = float64(v)
		case int64:
			fv = float64(v)
		default:
			continue
		}

		var match bool
		switch compare {
		case "eq":
			match = fv == value
		case "ne":
			match = fv != value
		case "gt":
			match = fv > value
		case "gte":
			match = fv >= value
		case "lt":
			match = fv < value
		case "lte":
			match = fv <= value
		default:
			match = false
		}

		if match {
			count++
		}
	}

	return count, nil
}

// ——— 内部工具（调用方必须持有写锁）———

func (r *InMemoryEventRepo) hasUniqueEventLocked(userID int64, eventType string) bool {
	userEvents, ok := r.uniqueIndex[userID]
	if !ok {
		return false
	}
	return userEvents[eventType]
}

func (r *InMemoryEventRepo) markUniqueLocked(userID int64, eventType string) {
	if _, ok := r.uniqueIndex[userID]; !ok {
		r.uniqueIndex[userID] = make(map[string]bool)
	}
	r.uniqueIndex[userID][eventType] = true
}
