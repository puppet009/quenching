// 班次活跃度/弃班回写链路单测：验证操作上报会续租、结束按终态映射原因、
// 弃班判定即时回收沙箱，以及"用户刚好同时提交"的竞态下不重复回收。
//
// 生命周期回写是 fire-and-forget（goroutine + 独立超时上下文），断言一律经
// 带缓冲 channel 收取，收不到即判失败。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// fakeLifecycle 记录沙箱侧收到的生命周期回写。
type fakeLifecycle struct {
	touches   chan struct{}
	completed chan string
	releases  chan string
}

func newFakeLifecycle() *fakeLifecycle {
	return &fakeLifecycle{
		touches:   make(chan struct{}, 8),
		completed: make(chan string, 8),
		releases:  make(chan string, 8),
	}
}

func (f *fakeLifecycle) TouchActivity(context.Context, int64) error {
	f.touches <- struct{}{}
	return nil
}

func (f *fakeLifecycle) MarkCompleted(_ context.Context, _ int64, reason string) error {
	f.completed <- reason
	return nil
}

func (f *fakeLifecycle) ReleaseNow(_ context.Context, _ int64, reason string) error {
	f.releases <- reason
	return nil
}

var _ SandboxLifecycle = (*fakeLifecycle)(nil)

// leaseFakeStore 只实现本用例触及的方法，其余方法继承嵌入接口（未实现即 nil 调用会
// 直接失败），以此保证测试链路没有意外走向未覆盖的仓储能力。
type leaseFakeStore struct {
	SessionStore

	abandonOK    bool
	abandonErr   error
	abandonCalls int

	finishStatus string
	finishOK     bool

	touched int
}

func (f *leaseFakeStore) AbandonSession(context.Context, int64) (bool, error) {
	f.abandonCalls++
	return f.abandonOK, f.abandonErr
}

func (f *leaseFakeStore) GetSessionStatus(context.Context, int64, int64) (string, bool, error) {
	return f.finishStatus, true, nil
}

func (f *leaseFakeStore) FinishSession(_ context.Context, in FinishSessionInput) (bool, error) {
	f.finishStatus = in.Status
	return f.finishOK, nil
}

func (f *leaseFakeStore) AddOperations(context.Context, int64, int64, []OperationInput) (int, error) {
	f.touched++
	return f.touched, nil
}

func newLeaseService(store *leaseFakeStore, lifecycle SandboxLifecycle) *SessionService {
	svc := NewSessionService(store, nil, nil)
	if lifecycle != nil {
		svc.SetSandboxLifecycle(lifecycle)
	}
	return svc
}

// expectWithin 在 d 内取一个值；超时视为"没有发生回写"。
func expectWithin[T any](t *testing.T, ch chan T, d time.Duration) (T, bool) {
	t.Helper()
	select {
	case v := <-ch:
		return v, true
	case <-time.After(d):
		var zero T
		return zero, false
	}
}

// expectNothing 断言 d 内没有任何回写到达。
func expectNothing[T any](t *testing.T, ch chan T, d time.Duration) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("不应发生回写，收到: %v", v)
	case <-time.After(d):
	}
}

func TestAbandonReleasesSandbox(t *testing.T) {
	store := &leaseFakeStore{abandonOK: true}
	lc := newFakeLifecycle()
	svc := newLeaseService(store, lc)

	ok, err := svc.Abandon(context.Background(), 77)
	if err != nil || !ok {
		t.Fatalf("弃班应成功: ok=%v err=%v", ok, err)
	}
	if store.abandonCalls != 1 {
		t.Fatalf("应调用一次 AbandonSession: %d", store.abandonCalls)
	}
	reason, got := expectWithin(t, lc.releases, time.Second)
	if !got {
		t.Fatal("弃班应立即回收沙箱")
	}
	if reason != sandboxReasonAbandoned {
		t.Fatalf("回收原因应为 abandoned，实得 %q", reason)
	}
}

func TestAbandonSkipsWhenAlreadyTerminal(t *testing.T) {
	store := &leaseFakeStore{abandonOK: false} // 用户在同一瞬间提交了修复
	lc := newFakeLifecycle()
	svc := newLeaseService(store, lc)

	ok, err := svc.Abandon(context.Background(), 78)
	if err != nil {
		t.Fatalf("已终态应返回 nil 错误: %v", err)
	}
	if ok {
		t.Fatal("已终态不应报告弃班成功")
	}
	expectNothing(t, lc.releases, 200*time.Millisecond)
}

func TestAbandonStoreErrorKeepsRetry(t *testing.T) {
	store := &leaseFakeStore{abandonErr: errors.New("db down")}
	lc := newFakeLifecycle()
	svc := newLeaseService(store, lc)

	if _, err := svc.Abandon(context.Background(), 79); err == nil {
		t.Fatal("仓储失败应透传，让扫描器下一轮重试")
	}
	expectNothing(t, lc.releases, 200*time.Millisecond)
}

// TestAbandonWithoutLifecycleWired 未配置 sandbox_service.base_url 时不得 panic。
func TestAbandonWithoutLifecycleWired(t *testing.T) {
	store := &leaseFakeStore{abandonOK: true}
	svc := newLeaseService(store, nil)

	ok, err := svc.Abandon(context.Background(), 80)
	if err != nil || !ok {
		t.Fatalf("未接线时弃班仍应落库: ok=%v err=%v", ok, err)
	}
}

func TestFinishMarksSandboxCompleted(t *testing.T) {
	cases := []struct {
		outcome model.SessionStatus
		want    string
	}{
		{model.StatusCompleted, sandboxReasonCompleted},
		{model.StatusAborted, sandboxReasonManual},
		{model.StatusFailed, sandboxReasonError},
	}
	for _, tc := range cases {
		t.Run(string(tc.outcome), func(t *testing.T) {
			store := &leaseFakeStore{finishStatus: string(model.StatusRunning), finishOK: true}
			lc := newFakeLifecycle()
			svc := newLeaseService(store, lc)

			if _, err := svc.Finish(context.Background(), 81, 9,
				FinishSessionRequest{Outcome: string(tc.outcome)}); err != nil {
				t.Fatalf("结束失败: %v", err)
			}
			reason, got := expectWithin(t, lc.completed, time.Second)
			if !got {
				t.Fatal("结束后应回写沙箱结束时刻")
			}
			if reason != tc.want {
				t.Fatalf("结束原因应为 %q，实得 %q", tc.want, reason)
			}
		})
	}
}

func TestAddOperationsRenewsLeaseWithThrottle(t *testing.T) {
	lc := newFakeLifecycle()
	store := &leaseFakeStore{}
	svc := newLeaseService(store, lc)
	// 把限流间隔拉到一年：同一会话第二次上报必被合并。
	svc.lease.interval = time.Hour
	ctx := context.Background()
	req := OperationsRequest{Operations: []OperationInput{{Type: "logs_view", Category: "observe"}}}

	if _, err := svc.AddOperations(ctx, 82, 9, req); err != nil {
		t.Fatalf("上报失败: %v", err)
	}
	if _, got := expectWithin(t, lc.touches, time.Second); !got {
		t.Fatal("首次操作上报应续租")
	}

	if _, err := svc.AddOperations(ctx, 82, 9, req); err != nil {
		t.Fatalf("上报失败: %v", err)
	}
	expectNothing(t, lc.touches, 200*time.Millisecond)
	if store.touched != 2 {
		t.Fatalf("两次上报都应落库（限流只作用于续租）: %d", store.touched)
	}
}

func TestFinishWithoutLifecycleWired(t *testing.T) {
	store := &leaseFakeStore{finishStatus: string(model.StatusRunning), finishOK: true}
	svc := newLeaseService(store, nil)

	if _, err := svc.Finish(context.Background(), 83, 9,
		FinishSessionRequest{Outcome: string(model.StatusAborted)}); err != nil {
		t.Fatalf("未接线时结束仍应成功: %v", err)
	}
}
