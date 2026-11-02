// session_activity.go —— 把"学员还在干活"回写给沙箱续租。
//
// 班次制下沙箱按最后一次活动续租（见 sandbox-service ActivityLease），
// 终端输入由沙箱侧 WS 直接刷新，工具面板里的观察类操作只能经操作上报到达这里。
// 上报频率高，故按会话限流 + 异步发送：续租失败最多退回兜底回收，不能拖慢实训链路。
package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// defaultActivityReportInterval 同一会话两次续租回写的最小间隔。
// 沙箱租期是分钟级，30s 粒度对判定精度无影响。
const defaultActivityReportInterval = 30 * time.Second

// activityLease 会话级续租回写器（限流 + fire-and-forget）。
type activityLease struct {
	client   SandboxLifecycle
	interval time.Duration
	timeout  time.Duration

	mu   sync.Mutex
	last map[int64]time.Time
}

// newActivityLease 创建续租回写器；client 为 nil 时所有方法空转。
func newActivityLease(client SandboxLifecycle, interval time.Duration) *activityLease {
	if interval <= 0 {
		interval = defaultActivityReportInterval
	}
	return &activityLease{
		client:   client,
		interval: interval,
		timeout:  5 * time.Second,
		last:     make(map[int64]time.Time),
	}
}

// touch 记录一次会话活动；未到间隔直接返回。
func (a *activityLease) touch(sessionID int64) {
	if a == nil || a.client == nil || sessionID <= 0 {
		return
	}
	now := time.Now()
	a.mu.Lock()
	last, seen := a.last[sessionID]
	if seen && now.Sub(last) < a.interval {
		a.mu.Unlock()
		return
	}
	a.last[sessionID] = now
	a.mu.Unlock()

	a.send(sessionID, func(ctx context.Context) error {
		return a.client.TouchActivity(ctx, sessionID)
	}, "续租回写")
}

// force 绕过限流立即回写（会话结束、弃班等一次性事件）。
func (a *activityLease) force(sessionID int64, fn func(context.Context) error, label string) {
	if a == nil || a.client == nil || sessionID <= 0 {
		return
	}
	a.mu.Lock()
	delete(a.last, sessionID)
	a.mu.Unlock()
	a.send(sessionID, fn, label)
}

// forget 清理会话限流状态。
func (a *activityLease) forget(sessionID int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	delete(a.last, sessionID)
	a.mu.Unlock()
}

func (a *activityLease) send(sessionID int64, fn func(context.Context) error, label string) {
	//nolint:contextcheck // 独立上下文：调用方 ctx 随 HTTP 请求结束取消，回写不应随之中断。
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
		defer cancel()
		if err := fn(ctx); err != nil {
			log.Printf("沙箱生命周期%s失败(session=%d): %v", label, sessionID, err)
		}
	}()
}
