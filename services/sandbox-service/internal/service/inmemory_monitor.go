package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
)

// InMemoryMonitorStore 内存版监控仓储（开发/测试用）。
// 实现 MonitorStore 接口，线程安全。
type InMemoryMonitorStore struct {
	mu            sync.RWMutex
	sandboxes     map[int64]*model.Sandbox // key: sessionID
	nextID        int64
	metrics       map[int64][]model.ResourceMetrics // key: sessionID
	anomalies     []*model.SandboxAnomaly
	nextAnomalyID int64
}

// NewInMemoryMonitorStore 创建内存监控仓储。
func NewInMemoryMonitorStore() *InMemoryMonitorStore {
	return &InMemoryMonitorStore{
		sandboxes: make(map[int64]*model.Sandbox),
		metrics:   make(map[int64][]model.ResourceMetrics),
		anomalies: make([]*model.SandboxAnomaly, 0),
		nextID:    1,
	}
}

// ——— 基础 Store 接口实现 ———

func (m *InMemoryMonitorStore) ListActive(ctx context.Context) ([]model.Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []model.Sandbox
	for _, sb := range m.sandboxes {
		if sb.IsActiveState() {
			result = append(result, *sb)
		}
	}
	return result, nil
}

func (m *InMemoryMonitorStore) ListAllForReap(ctx context.Context) ([]model.Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []model.Sandbox
	for _, sb := range m.sandboxes {
		if sb.Status != model.StatusReleased {
			result = append(result, *sb)
		}
	}
	return result, nil
}

func (m *InMemoryMonitorStore) MarkStatus(ctx context.Context, sessionID int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sb, ok := m.sandboxes[sessionID]
	if !ok {
		return fmt.Errorf("沙箱不存在")
	}
	sb.Status = status
	return nil
}

func (m *InMemoryMonitorStore) CountActiveByUser(ctx context.Context, userID int64) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, sb := range m.sandboxes {
		if sb.UserID == userID && sb.IsActiveState() {
			count++
		}
	}
	return count, nil
}

func (m *InMemoryMonitorStore) Create(ctx context.Context, sb *model.Sandbox) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sb.ID == 0 {
		sb.ID = m.nextID
		m.nextID++
	}
	clone := *sb
	m.sandboxes[sb.SessionID] = &clone
	return nil
}

// ——— MonitorStore 扩展接口 ———

func (m *InMemoryMonitorStore) List(_ context.Context, q model.SandboxQuery) (*model.SandboxPage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []*model.Sandbox
	for _, sb := range m.sandboxes {
		// 用户筛选
		if q.UserID > 0 && sb.UserID != q.UserID {
			continue
		}
		// 项目筛选
		if q.ProjectID > 0 && sb.ProjectID != q.ProjectID {
			continue
		}
		// 状态筛选
		if q.Status != "" && sb.Status != q.Status {
			continue
		}
		// 命名空间搜索
		if q.Namespace != "" && !containsSubstr(sb.Namespace, q.Namespace) {
			continue
		}
		// 活跃状态筛选
		if q.IsActive != nil {
			active := sb.IsActiveState()
			if *q.IsActive != active {
				continue
			}
		}
		filtered = append(filtered, sb)
	}

	// 按创建时间倒序（最新的在前）
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	total := len(filtered)

	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= total {
		return &model.SandboxPage{Items: []model.Sandbox{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	if end > total {
		end = total
	}

	items := make([]model.Sandbox, 0, end-start)
	for _, sb := range filtered[start:end] {
		items = append(items, *sb)
	}

	return &model.SandboxPage{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (m *InMemoryMonitorStore) GetBySessionID(_ context.Context, sessionID int64) (*model.Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sb, ok := m.sandboxes[sessionID]
	if !ok {
		return nil, ErrSandboxNotFound.Unwrap()
	}
	clone := *sb
	return &clone, nil
}

func (m *InMemoryMonitorStore) GetByID(_ context.Context, id int64) (*model.Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sb := range m.sandboxes {
		if sb.ID == id {
			clone := *sb
			return &clone, nil
		}
	}
	return nil, ErrSandboxNotFound.Unwrap()
}

func (m *InMemoryMonitorStore) CountByStatus(_ context.Context) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]int)
	for _, sb := range m.sandboxes {
		result[sb.Status]++
	}
	return result, nil
}

func (m *InMemoryMonitorStore) CountActive(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, sb := range m.sandboxes {
		if sb.IsActiveState() {
			count++
		}
	}
	return count, nil
}

// ——— 资源监控 ———

func (m *InMemoryMonitorStore) SaveMetrics(_ context.Context, metrics *model.ResourceMetrics) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if metrics.Timestamp.IsZero() {
		metrics.Timestamp = time.Now()
	}

	list := m.metrics[metrics.SessionID]
	list = append(list, *metrics)
	// 最多保留 1000 条（防止内存无限增长）
	if len(list) > 1000 {
		list = list[len(list)-1000:]
	}
	m.metrics[metrics.SessionID] = list
	return nil
}

func (m *InMemoryMonitorStore) GetLatestMetrics(_ context.Context, sessionID int64) (*model.ResourceMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.metrics[sessionID]
	if len(list) == 0 {
		return nil, nil
	}
	latest := list[len(list)-1]
	return &latest, nil
}

func (m *InMemoryMonitorStore) ListMetrics(_ context.Context, sessionID int64, limit int) ([]model.ResourceMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.metrics[sessionID]
	if len(list) == 0 {
		return []model.ResourceMetrics{}, nil
	}
	if limit <= 0 || limit > len(list) {
		limit = len(list)
	}
	// 返回最近的 limit 条
	result := make([]model.ResourceMetrics, limit)
	copy(result, list[len(list)-limit:])
	return result, nil
}

// ——— 异常告警 ———

func (m *InMemoryMonitorStore) RecordAnomaly(_ context.Context, anomaly *model.SandboxAnomaly) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextAnomalyID++
	anomaly.ID = m.nextAnomalyID
	if anomaly.DetectedAt.IsZero() {
		anomaly.DetectedAt = time.Now()
	}

	clone := *anomaly
	m.anomalies = append(m.anomalies, &clone)
	return nil
}

func (m *InMemoryMonitorStore) ListAnomalies(_ context.Context, q model.AnomalyQuery) (*model.AnomalyPage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []*model.SandboxAnomaly
	for _, a := range m.anomalies {
		if q.Level != "" && a.Level != q.Level {
			continue
		}
		if q.AnomalyType != "" && a.AnomalyType != q.AnomalyType {
			continue
		}
		if q.Resolved != nil && a.Resolved != *q.Resolved {
			continue
		}
		filtered = append(filtered, a)
	}

	// 按检测时间倒序
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].DetectedAt.After(filtered[j].DetectedAt)
	})

	total := len(filtered)

	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= total {
		return &model.AnomalyPage{Items: []model.SandboxAnomaly{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	if end > total {
		end = total
	}

	items := make([]model.SandboxAnomaly, 0, end-start)
	for _, a := range filtered[start:end] {
		items = append(items, *a)
	}

	return &model.AnomalyPage{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (m *InMemoryMonitorStore) ResolveAnomaly(_ context.Context, anomalyID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, a := range m.anomalies {
		if a.ID == anomalyID {
			a.Resolved = true
			a.ResolvedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("异常记录不存在")
}

func (m *InMemoryMonitorStore) CountUnresolvedAnomalies(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, a := range m.anomalies {
		if !a.Resolved {
			count++
		}
	}
	return count, nil
}

// ——— 手动释放 ———

func (m *InMemoryMonitorStore) Release(_ context.Context, sessionID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sb, ok := m.sandboxes[sessionID]
	if !ok {
		return ErrSandboxNotFound.Unwrap()
	}
	if sb.Status == model.StatusReleased || sb.Status == model.StatusReleasing {
		return ErrSandboxConflict.Unwrap()
	}
	sb.Status = model.StatusReleasing
	return nil
}

// ——— 活跃度续租 / 结束回写 ———

func (m *InMemoryMonitorStore) TouchActivity(_ context.Context, sessionID int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sb, ok := m.sandboxes[sessionID]
	if !ok || sb.Status == model.StatusReleased {
		return nil // 与 PG 实现一致：迟到信号静默丢弃
	}
	if sb.LastActivityAt.Before(at) {
		sb.LastActivityAt = at
	}
	return nil
}

func (m *InMemoryMonitorStore) MarkCompleted(_ context.Context, sessionID int64, at time.Time, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sb, ok := m.sandboxes[sessionID]
	if !ok {
		return ErrSandboxNotFound.Unwrap()
	}
	if sb.CompletedAt.IsZero() {
		sb.CompletedAt = at
	}
	return nil
}

// ——— 工具函数 ———

func containsSubstr(s, sub string) bool {
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
