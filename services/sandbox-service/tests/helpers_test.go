package tests

import (
	"context"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// kubeClient 别名，便于测试构造。
type kubeClient = *service.K8sClient

// fakeStore 内存版 Store，用于单测调谐器/TTL/配额，不依赖 PG。
type fakeStore struct {
	active []model.Sandbox
	all    []model.Sandbox
	status map[int64]string
	nextID int64
}

// newFakeStore 构造一个空的内存 Store。
func newFakeStore() *fakeStore {
	return &fakeStore{status: map[int64]string{}}
}

func (f *fakeStore) ListActive(_ context.Context) ([]model.Sandbox, error) {
	return f.active, nil
}

func (f *fakeStore) ListAllForReap(_ context.Context) ([]model.Sandbox, error) {
	return f.all, nil
}

func (f *fakeStore) MarkStatus(_ context.Context, sessionID int64, status string) error {
	if f.status == nil {
		f.status = map[int64]string{}
	}
	f.status[sessionID] = status
	return nil
}

// CountActiveByUser 统计用户活跃沙箱数（状态非 released 的都算）。
func (f *fakeStore) CountActiveByUser(_ context.Context, userID int64) (int, error) {
	count := 0
	for _, sb := range f.all {
		if sb.UserID != userID {
			continue
		}
		if sb.Status == model.StatusReleased {
			continue
		}
		count++
	}
	return count, nil
}

// Create 新建沙箱记录（内存模拟，自增 ID）。
func (f *fakeStore) Create(_ context.Context, sb *model.Sandbox) error {
	f.nextID++
	sb.ID = f.nextID
	if sb.Status == "" {
		sb.Status = model.StatusCreating
	}
	if sb.CreatedAt.IsZero() {
		sb.CreatedAt = time.Now()
	}
	if sb.Namespace == "" {
		sb.Namespace = model.NamespaceName(sb.SessionID)
	}
	f.all = append(f.all, *sb)
	// active 列表只放非终态的
	if sb.Status != model.StatusReleased {
		f.active = append(f.active, *sb)
	}
	return nil
}

// mockResourceChecker 资源检查器的 mock 实现，用于单测控制资源充足/不足。
type mockResourceChecker struct {
	enough bool // 是否有足够资源
	err    error
}

func (m *mockResourceChecker) HasEnoughResource(_ context.Context) (bool, error) {
	return m.enough, m.err
}

func newTestReconciler(client *service.K8sClient, store service.Store) *service.Reconciler {
	return service.NewReconciler(client, store)
}

// GetBySessionID 按会话ID查沙箱（测试用）。
func (f *fakeStore) GetBySessionID(_ context.Context, sessionID int64) (*model.Sandbox, error) {
	for i := range f.all {
		if f.all[i].SessionID == sessionID {
			// 同步 status map 的最新状态
			if s, ok := f.status[sessionID]; ok {
				sb := f.all[i]
				sb.Status = s
				return &sb, nil
			}
			return &f.all[i], nil
		}
	}
	return nil, nil
}
