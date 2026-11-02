// activity.go 沙箱活跃度信号：按活动续租、实训结束回写。
//
// 班次制下沙箱不能按"创建时间 + 固定 TTL"回收——学员做得久就该一直活着；
// 反过来，人走了沙箱也不能永驻（单机只有 2 个槽位）。因此回收判定改挂在
// "最后一次活动"上：终端输入、观察类操作都会刷新它。
//
// 高频信号（每次按键、每条操作上报）必须合并成低频写库，故有 ActivityTracker。
package service

import (
	"context"
	"sync"
	"time"
)

// ActivityPort 活跃度落库端口（PGStore 与内存 Store 都实现）。
type ActivityPort interface {
	// TouchActivity 把最后活动时间向后推进。
	TouchActivity(ctx context.Context, sessionID int64, at time.Time) error
	// MarkCompleted 回写实训结束时刻与原因（进入结束宽限期）。
	MarkCompleted(ctx context.Context, sessionID int64, at time.Time, reason string) error
}

// defaultTouchInterval 同一会话两次写库的最小间隔。
// 回收租期是分钟级（ActivityLease=30min），30s 粒度足够，且能挡住终端连续按键。
const defaultTouchInterval = 30 * time.Second

// ActivityTracker 活动信号限流器：进程内记录每个会话的最近落库时间，
// 未到间隔只更新内存，写库放到独立 goroutine，不阻塞调用方（WS 读循环 / HTTP handler）。
type ActivityTracker struct {
	port     ActivityPort
	interval time.Duration
	clock    Clock

	mu   sync.Mutex
	last map[int64]time.Time
}

// NewActivityTracker 创建限流器；interval<=0 用默认 30s，clock 为空用系统时钟。
func NewActivityTracker(port ActivityPort, clock Clock, interval time.Duration) *ActivityTracker {
	if clock == nil {
		clock = NewSystemClock()
	}
	if interval <= 0 {
		interval = defaultTouchInterval
	}
	return &ActivityTracker{port: port, interval: interval, clock: clock, last: make(map[int64]time.Time)}
}

// Touch 记录一次活动。距上次落库不足 interval 时直接返回。
func (t *ActivityTracker) Touch(ctx context.Context, sessionID int64) {
	if t == nil || sessionID <= 0 {
		return
	}
	now := t.clock.Now()
	t.mu.Lock()
	last, seen := t.last[sessionID]
	if seen && now.Sub(last) < t.interval {
		t.mu.Unlock()
		return
	}
	t.last[sessionID] = now
	t.mu.Unlock()

	t.write(ctx, sessionID, now)
}

// TouchNow 绕过限流立即落库（终端连接关闭时补最后一次活动）。
func (t *ActivityTracker) TouchNow(ctx context.Context, sessionID int64) {
	if t == nil || sessionID <= 0 {
		return
	}
	now := t.clock.Now()
	t.mu.Lock()
	t.last[sessionID] = now
	t.mu.Unlock()
	t.write(ctx, sessionID, now)
}

// Forget 清理会话的限流状态（沙箱释放后调用，map 不无限增长）。
func (t *ActivityTracker) Forget(sessionID int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.last, sessionID)
	t.mu.Unlock()
}

// MarkCompleted 回写实训结束时刻，使沙箱进入结束宽限期。
func (t *ActivityTracker) MarkCompleted(ctx context.Context, sessionID int64, reason string) error {
	if t == nil {
		return nil
	}
	return t.port.MarkCompleted(ctx, sessionID, t.clock.Now(), reason)
}

func (t *ActivityTracker) write(ctx context.Context, sessionID int64, at time.Time) {
	// 活动写失败只影响续租精度（最坏退回按创建时间判定），不值得拖慢终端链路。
	//nolint:contextcheck // 派生独立上下文：调用方 ctx 随 WS 连接结束取消。
	go func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = t.port.TouchActivity(cctx, sessionID, at)
	}()
}

// 结束原因（与 sandboxes.destroy_reason 口径一致）。
const (
	ReasonCompleted = "completed" // 实训正常完成
	ReasonAbandoned = "abandoned" // 弃班（长时间无活动，系统判定结束）
	ReasonUserExit  = "manual"    // 用户主动离开
)
