// 活跃度限流器单测：验证高频活动信号按间隔合并落库、绕过限流、清理状态，
// 以及结束时刻直接透传到端口（不节流——错过一次结束回写就意味着沙箱多占一个槽位）。
package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

// activityFake 记录端口被调用的时刻（异步写，用 channel 传递以便断言）。
type activityFake struct {
	touches   chan time.Time
	completed chan time.Time
	err       error
}

func newActivityFake() *activityFake {
	return &activityFake{
		touches:   make(chan time.Time, 16),
		completed: make(chan time.Time, 16),
	}
}

func (f *activityFake) TouchActivity(_ context.Context, _ int64, at time.Time) error {
	f.touches <- at
	return f.err
}

func (f *activityFake) MarkCompleted(_ context.Context, _ int64, at time.Time, _ string) error {
	f.completed <- at
	return f.err
}

// stepClock 可手动推进的假时钟。
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *stepClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// oneTouch 取出一次落库时刻；wait 内没有写入则失败（异步写必须等到）。
func oneTouch(t *testing.T, f *activityFake, wait time.Duration) (time.Time, bool) {
	t.Helper()
	select {
	case at := <-f.touches:
		return at, true
	case <-time.After(wait):
		return time.Time{}, false
	}
}

const touchInterval = 50 * time.Millisecond

func TestActivityTrackerThrottles(t *testing.T) {
	fake := newActivityFake()
	clock := &stepClock{t: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)}
	tr := NewActivityTracker(fake, clock, touchInterval)
	ctx := context.Background()

	tr.Touch(ctx, 7)
	first, ok := oneTouch(t, fake, time.Second)
	if !ok {
		t.Fatal("首次活动应落库")
	}
	if !first.Equal(clock.Now()) {
		t.Fatalf("落库时刻应为时钟当前值: %v", first)
	}

	// 间隔内的高频按键/上报只更新内存，不再写库。
	for i := 0; i < 20; i++ {
		tr.Touch(ctx, 7)
	}
	clock.add(touchInterval / 2)
	tr.Touch(ctx, 7)
	select {
	case at := <-fake.touches:
		t.Fatalf("未到一个间隔不应写库: %v", at)
	case <-time.After(200 * time.Millisecond):
	}

	// 间隔过后再次活动 → 续租写库。
	clock.add(touchInterval)
	tr.Touch(ctx, 7)
	if _, ok := oneTouch(t, fake, time.Second); !ok {
		t.Fatal("超过间隔后应再次落库续租")
	}
}

func TestActivityTrackerTouchNowBypassesThrottle(t *testing.T) {
	fake := newActivityFake()
	clock := &stepClock{t: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)}
	tr := NewActivityTracker(fake, clock, time.Hour)
	ctx := context.Background()

	tr.Touch(ctx, 7)
	if _, ok := oneTouch(t, fake, time.Second); !ok {
		t.Fatal("首次 Touch 应落库")
	}

	// 终端关闭时补最后一次活动：即便仍在间隔内也必须立即写库。
	clock.add(time.Minute)
	tr.TouchNow(ctx, 7)
	at, ok := oneTouch(t, fake, time.Second)
	if !ok {
		t.Fatal("TouchNow 应绕过限流立即落库")
	}
	if !at.Equal(clock.Now()) {
		t.Fatalf("TouchNow 应写入当前时刻: %v", at)
	}
}

func TestActivityTrackerForgetResetsThrottle(t *testing.T) {
	fake := newActivityFake()
	clock := &stepClock{t: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)}
	tr := NewActivityTracker(fake, clock, time.Hour)
	ctx := context.Background()

	tr.Touch(ctx, 7)
	if _, ok := oneTouch(t, fake, time.Second); !ok {
		t.Fatal("首次 Touch 应落库")
	}
	// 未 Forget 时同会话再次活动被限流。
	tr.Touch(ctx, 7)
	select {
	case <-fake.touches:
		t.Fatal("限流期内不应写库")
	case <-time.After(100 * time.Millisecond):
	}

	tr.Forget(7)
	tr.Touch(ctx, 7)
	if _, ok := oneTouch(t, fake, time.Second); !ok {
		t.Fatal("Forget 后应视为新会话立即落库")
	}
}

func TestActivityTrackerIgnoresInvalidSession(t *testing.T) {
	fake := newActivityFake()
	tr := NewActivityTracker(fake, &stepClock{}, time.Millisecond)

	tr.Touch(context.Background(), 0)
	tr.TouchNow(context.Background(), -1)
	select {
	case <-fake.touches:
		t.Fatal("非法会话 ID 不应写库")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestActivityTrackerMarkCompleted(t *testing.T) {
	fake := newActivityFake()
	clock := &stepClock{t: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)}
	tr := NewActivityTracker(fake, clock, time.Hour)

	if err := tr.MarkCompleted(context.Background(), 7, ReasonAbandoned); err != nil {
		t.Fatalf("结束回写失败: %v", err)
	}
	select {
	case at := <-fake.completed:
		if !at.Equal(clock.Now()) {
			t.Fatalf("结束时刻应为时钟当前值: %v", at)
		}
	case <-time.After(time.Second):
		t.Fatal("MarkCompleted 应同步透传到端口")
	}
}

// TestNilTrackerSafe 未接线（nil tracker）时不得 panic——本地/降级部署会传 nil。
func TestNilTrackerSafe(t *testing.T) {
	var tr *ActivityTracker
	tr.Touch(context.Background(), 7)
	tr.TouchNow(context.Background(), 7)
	tr.Forget(7)
	if err := tr.MarkCompleted(context.Background(), 7, ReasonCompleted); err != nil {
		t.Fatalf("nil tracker 应安全返回: %v", err)
	}
}
