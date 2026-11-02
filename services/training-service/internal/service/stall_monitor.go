// 卡顿状态扫描器（T3-6）。
//
// 周期性扫描所有 running 训练会话，调用 StallDetector 做纯规则评估，并把结果保存在
// 内存 map 中（不落库、不推送）。状态翻转时触发预留钩子：
//   - OnStall：正常 → 卡顿（T3-7 主动线索推送的挂载点）；
//   - OnRecover：卡顿 → 正常；
//   - OnAbandon：R3 连续空闲超时限，判弃班（每会话成功一次，见 checkAbandon）。
//
// 会话离开 running（结束/中止）或从列表消失时直接清理内存状态；进程重启后
// 下一轮扫描即可重新算出全部状态，无需持久化。
package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// defaultScanInterval 默认扫描周期，与告警管理器一致。
const defaultScanInterval = 30 * time.Second

// StallPort 扫描器的数据端口：把 PG/Prometheus 访问隔离在 service 层之外，
// 生产实现见 repository.StallRepo，测试可用内存 fake。
type StallPort interface {
	// ListRunning 列出当前所有 running（且未删除）的会话。
	ListRunning(ctx context.Context) ([]model.StallSessionInfo, error)
	// Activity 取会话在 [since, now] 内的有效活动：
	// 最近一次活动时间（从无活动返回 nil）与窗口内活动次数。
	// 有效活动 = session_operations 任意行 + ai_messages 中用户提问（去重后计数）。
	Activity(ctx context.Context, sessionID int64, since, now time.Time) (last *time.Time, count int, err error)
	// MetricWindows 取三指标在 [start, end] 的窗口序列；查询失败时各窗口 Available=false。
	MetricWindows(ctx context.Context, sessionID int64, start, end time.Time) (model.StallMetricWindows, error)
}

// StallHook 状态翻转回调。实现必须快速返回或自行异步化，避免阻塞扫描循环；
// 钩子在扫描器持锁外调用，可安全读取 StallMonitor 状态。
type StallHook func(ctx context.Context, st model.StallStatus)

// AbandonHook 弃班（R3）回调，入参为会话最小信息（含归属用户/项目，供触发生成复盘）
// 与命中那一轮的评估结果（含空闲时长，供日志）。
// 返回非 nil 表示本轮弃班处理失败：扫描器会撤销去重标记，下一轮重新触发（重试由
// 实现自己保证幂等）；返回 nil 则该会话不再重复回调。同样必须快速返回或自行异步化。
type AbandonHook func(ctx context.Context, info model.StallSessionInfo, st model.StallStatus) error

// StallMonitor 卡顿状态扫描器。
type StallMonitor struct {
	det   *StallDetector
	port  StallPort
	hooks struct {
		onStall   StallHook
		onRecover StallHook
		onAbandon AbandonHook
	}

	interval time.Duration

	mu        sync.RWMutex
	statuses  map[int64]model.StallStatus
	infos     map[int64]model.StallSessionInfo
	abandoned map[int64]bool

	stopChan chan struct{}
	stopOnce sync.Once
}

// NewStallMonitor 创建扫描器；interval<=0 时用默认 30s。
func NewStallMonitor(det *StallDetector, port StallPort, interval time.Duration) *StallMonitor {
	if interval <= 0 {
		interval = defaultScanInterval
	}
	return &StallMonitor{
		det:       det,
		port:      port,
		interval:  interval,
		statuses:  make(map[int64]model.StallStatus),
		infos:     make(map[int64]model.StallSessionInfo),
		abandoned: make(map[int64]bool),
		stopChan:  make(chan struct{}),
	}
}

// OnStall 注册"正常 → 卡顿"钩子（供 T3-7 接线，当前可为 nil）。
func (m *StallMonitor) OnStall(h StallHook) { m.hooks.onStall = h }

// OnRecover 注册"卡顿 → 正常"钩子（供 T3-7 接线，当前可为 nil）。
func (m *StallMonitor) OnRecover(h StallHook) { m.hooks.onRecover = h }

// OnAbandon 注册"连续空闲超时限 → 弃班"钩子（会话置终止 + 回收沙箱 + 未完成复盘）。
func (m *StallMonitor) OnAbandon(h AbandonHook) { m.hooks.onAbandon = h }

// Start 启动周期扫描：先立即评估一轮（避免重启后最长 30s 空窗），再按周期执行，
// 直到 ctx 取消或 Stop()。
func (m *StallMonitor) Start(ctx context.Context) {
	m.EvaluateNow(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.EvaluateNow(ctx)
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		}
	}
}

// Stop 停止后台循环（可重复调用）。
func (m *StallMonitor) Stop() {
	m.stopOnce.Do(func() { close(m.stopChan) })
}

// EvaluateNow 立即执行一轮全量扫描。单会话/单查询出错只记日志并保留旧状态，
// 不影响其他会话；整个 running 列表查询失败则本轮跳过。
func (m *StallMonitor) EvaluateNow(ctx context.Context) {
	sessions, err := m.port.ListRunning(ctx)
	if err != nil {
		log.Printf("卡顿扫描: 列出 running 会话失败，本轮跳过: %v", err)
		return
	}

	active := make(map[int64]bool, len(sessions))
	for _, s := range sessions {
		active[s.ID] = true
		m.remember(s)
		st := m.evaluateOne(ctx, s)
		if st.SessionID == 0 {
			continue
		}
		m.merge(ctx, st)
		m.checkAbandon(ctx, s, st)
	}
	m.sweep(active)
}

// remember 缓存本轮 running 会话的最小信息，供弃班钩子取归属用户/项目。
func (m *StallMonitor) remember(s model.StallSessionInfo) {
	m.mu.Lock()
	m.infos[s.ID] = s
	m.mu.Unlock()
}

// checkAbandon R3 命中时触发弃班回调。同一会话只成功回调一次：钩子把会话置终态前，
// 下一轮扫描它仍在 running 列表里，不去重会反复回收沙箱、重复投复盘任务。
// 回调失败则撤销标记，下一轮重试（失败时会话仍是 running，沙箱活跃度租约兜底回收）。
func (m *StallMonitor) checkAbandon(ctx context.Context, s model.StallSessionInfo, st model.StallStatus) {
	if !st.Abandoned || m.hooks.onAbandon == nil {
		return
	}

	m.mu.Lock()
	if m.abandoned[s.ID] {
		m.mu.Unlock()
		return
	}
	m.abandoned[s.ID] = true
	m.mu.Unlock()

	if err := m.hooks.onAbandon(ctx, s, st); err != nil {
		log.Printf("卡顿扫描: 会话 %d 弃班处理失败，下一轮重试: %v", s.ID, err)
		m.mu.Lock()
		delete(m.abandoned, s.ID)
		m.mu.Unlock()
	}
}

// evaluateOne 评估单会话。R1 命中时跳过 Prometheus 查询（空闲会话无需查指标）；
// 任一步骤出错返回零值 StallStatus，调用方保留旧状态。
func (m *StallMonitor) evaluateOne(ctx context.Context, s model.StallSessionInfo) model.StallStatus {
	now := time.Now()
	window := m.det.Config().ProgressWindow
	since := now.Add(-window)

	last, count, err := m.port.Activity(ctx, s.ID, since, now)
	if err != nil {
		log.Printf("卡顿扫描: 取会话 %d 活动失败，保留旧状态: %v", s.ID, err)
		return model.StallStatus{}
	}

	in := model.StallInput{
		Now:              now,
		StartedAt:        s.StartedAt,
		WindowActivities: count,
	}
	if last != nil {
		in.LastActivityAt = *last
	}

	// R1 快路径：已构成空闲卡顿时不必查三指标，直接评估（R1 在 Evaluate 内短路返回）。
	if _, idle := m.det.EvaluateIdle(in); idle {
		return m.det.Evaluate(s.ID, in)
	}

	// R1 未命中：仅在窗口活动达到门槛时才值得查指标。
	if count >= m.det.Config().MinActivities {
		w, err := m.port.MetricWindows(ctx, s.ID, since, now)
		if err != nil {
			// 与保守原则一致：查不到指标时以 Available=false 评估，R2 弃权不误报。
			log.Printf("卡顿扫描: 会话 %d 指标窗口查询失败，R2 本轮弃权: %v", s.ID, err)
		} else {
			in.ErrorRate = w.ErrorRate
			in.P99Latency = w.P99Latency
			in.QPS = w.QPS
		}
	}
	return m.det.Evaluate(s.ID, in)
}

// merge 写入新状态并检测翻转；持续卡顿时保留首次判卡顿的 Since。
func (m *StallMonitor) merge(ctx context.Context, latest model.StallStatus) {
	var prev model.StallStatus
	var existed bool

	m.mu.Lock()
	if prev, existed = m.statuses[latest.SessionID]; existed &&
		prev.State == model.StallStateStalled && latest.State == model.StallStateStalled {
		latest.Since = prev.Since // 连续卡顿：Since 保持首次命中时刻。
	}
	m.statuses[latest.SessionID] = latest
	m.mu.Unlock()

	if !existed {
		if latest.State == model.StallStateStalled && m.hooks.onStall != nil {
			m.hooks.onStall(ctx, latest)
		}
		return
	}
	if prev.State == latest.State {
		return
	}
	switch latest.State {
	case model.StallStateStalled:
		if m.hooks.onStall != nil {
			m.hooks.onStall(ctx, latest)
		}
	case model.StallStateNormal:
		if m.hooks.onRecover != nil {
			m.hooks.onRecover(ctx, latest)
		}
	}
}

// sweep 清理已不在 running 列表的会话状态（会话结束/中止/弃班），map 不无限增长。
func (m *StallMonitor) sweep(active map[int64]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.statuses {
		if !active[id] {
			delete(m.statuses, id)
		}
	}
	for id := range m.infos {
		if !active[id] {
			delete(m.infos, id)
		}
	}
	for id := range m.abandoned {
		if !active[id] {
			delete(m.abandoned, id)
		}
	}
}

// Status 查单会话当前卡顿状态；尚未扫描/会话不存在时 ok=false。
func (m *StallMonitor) Status(sessionID int64) (model.StallStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.statuses[sessionID]
	return st, ok
}

// ListStatuses 返回全部已扫描会话状态（按会话 ID 升序），供管理/验证接口使用。
func (m *StallMonitor) ListStatuses() []model.StallStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.StallStatus, 0, len(m.statuses))
	for _, st := range m.statuses {
		out = append(out, st)
	}
	// map 遍历无序，输出前按会话 ID 排序保证稳定。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].SessionID > out[j].SessionID; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
