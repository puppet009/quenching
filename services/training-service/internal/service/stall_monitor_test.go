// 卡顿扫描器单测（T3-6）：用内存 fake 端口验证状态翻转、Since 保持、
// 终态清理与"取数失败保留旧状态"，不起真实后台循环（直接调 EvaluateNow）。
// 弃班（R3）另验证：每会话成功回调一次、失败后下一轮重试、未达时限不触发。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// fakePort 可控的内存数据端口。
type fakePort struct {
	sessions   []model.StallSessionInfo
	lastAct    map[int64]*time.Time // nil/缺键表示从无活动
	actCount   map[int64]int
	windows    map[int64]model.StallMetricWindows
	winErr     map[int64]bool
	listErr    error
	actErr     map[int64]bool
	winQueries int // MetricWindows 被调用次数，用于验证 R1 命中时不查指标
}

func newFakePort() *fakePort {
	return &fakePort{
		lastAct:  map[int64]*time.Time{},
		actCount: map[int64]int{},
		windows:  map[int64]model.StallMetricWindows{},
		winErr:   map[int64]bool{},
		actErr:   map[int64]bool{},
	}
}

func (f *fakePort) ListRunning(_ context.Context) ([]model.StallSessionInfo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.sessions, nil
}

func (f *fakePort) Activity(_ context.Context, id int64, _, _ time.Time) (*time.Time, int, error) {
	if f.actErr[id] {
		return nil, 0, errors.New("activity boom")
	}
	return f.lastAct[id], f.actCount[id], nil
}

func (f *fakePort) MetricWindows(_ context.Context, id int64, _, _ time.Time) (model.StallMetricWindows, error) {
	f.winQueries++
	if f.winErr[id] {
		return model.StallMetricWindows{}, errors.New("prom boom")
	}
	return f.windows[id], nil
}

// flatWindow 造 4 个等间隔点、值恒定的可用窗口。
func flatWindow(now time.Time, v float64) model.StallMetricWindow {
	return model.StallMetricWindow{Available: true, Points: points(now.Add(-10*time.Minute), v, v, v, v)}
}

func newTestMonitor(port StallPort) (*StallMonitor, *int, *int) {
	det := NewStallDetector(model.StallConfig{}) // 默认 5 分钟空闲 / 10 分钟窗口 / 2 次活动
	m := NewStallMonitor(det, port, time.Second)
	var stalls, recovers int
	m.OnStall(func(_ context.Context, _ model.StallStatus) { stalls++ })
	m.OnRecover(func(_ context.Context, _ model.StallStatus) { recovers++ })
	return m, &stalls, &recovers
}

func TestMonitorStallRecoverSweep(t *testing.T) {
	ctx := context.Background()
	port := newFakePort()
	m, stalls, recovers := newTestMonitor(port)

	// 会话 1：8 分钟前开训、从无活动 → R1 空闲卡顿。
	port.sessions = []model.StallSessionInfo{{ID: 1, StartedAt: time.Now().Add(-8 * time.Minute)}}
	m.EvaluateNow(ctx)

	st, ok := m.Status(1)
	if !ok || st.State != model.StallStateStalled || st.Reason != model.StallReasonIdle {
		t.Fatalf("首轮应判空闲卡顿: ok=%v st=%+v", ok, st)
	}
	if *stalls != 1 || *recovers != 0 {
		t.Fatalf("翻转钩子计数错误: stall=%d recover=%d", *stalls, *recovers)
	}
	if port.winQueries != 0 {
		t.Fatal("R1 命中时不应查询指标窗口")
	}
	firstSince := st.Since

	// 第二轮：仍然空闲 → 持续卡顿，Since 保持，钩子不重复触发。
	time.Sleep(10 * time.Millisecond)
	m.EvaluateNow(ctx)
	st, _ = m.Status(1)
	if st.State != model.StallStateStalled || !st.Since.Equal(firstSince) {
		t.Fatalf("持续卡顿应保持首次 Since: %+v vs %v", st, firstSince)
	}
	if *stalls != 1 {
		t.Fatalf("持续卡顿不应重复触发 OnStall: %d", *stalls)
	}

	// 第三轮：用户刚操作过（1 分钟前），窗口内 3 次活动；指标不可用 → R2 弃权 → 正常。
	now := time.Now()
	port.lastAct[1] = &now
	port.actCount[1] = 3
	m.EvaluateNow(ctx)
	st, _ = m.Status(1)
	if st.State != model.StallStateNormal {
		t.Fatalf("活动恢复且 R2 弃权时应回到正常: %+v", st)
	}
	if *recovers != 1 {
		t.Fatalf("应触发一次 OnRecover: %d", *recovers)
	}

	// 第四轮：会话结束消失 → 内存状态被清理。
	port.sessions = nil
	m.EvaluateNow(ctx)
	if _, ok := m.Status(1); ok {
		t.Fatal("会话离开 running 后状态应被清理")
	}
}

func TestMonitorR2NoProgressThenImprove(t *testing.T) {
	ctx := context.Background()
	port := newFakePort()
	m, stalls, recovers := newTestMonitor(port)

	now := time.Now()
	last := now.Add(-2 * time.Minute) // R1 不命中
	port.sessions = []model.StallSessionInfo{{ID: 9, StartedAt: now.Add(-30 * time.Minute)}}
	port.lastAct[9] = &last
	port.actCount[9] = 3

	// 三指标首末段持平 → R2 无进展卡顿。
	flat := model.StallMetricWindows{
		ErrorRate:  flatWindow(now, 5),
		P99Latency: flatWindow(now, 0.5),
		QPS:        flatWindow(now, 10),
	}
	port.windows[9] = flat
	m.EvaluateNow(ctx)
	st, ok := m.Status(9)
	if !ok || st.Reason != model.StallReasonNoProgress {
		t.Fatalf("应判无进展卡顿: ok=%v st=%+v", ok, st)
	}
	if *stalls != 1 {
		t.Fatalf("应触发一次 OnStall: %d", *stalls)
	}

	// 错误率从 5 降到 3（降 2pp >= 1pp）→ 有进展，恢复。
	improved := flat
	improved.ErrorRate = model.StallMetricWindow{Available: true,
		Points: points(now.Add(-10*time.Minute), 5, 5, 3, 3)}
	port.windows[9] = improved
	m.EvaluateNow(ctx)
	st, _ = m.Status(9)
	if st.State != model.StallStateNormal {
		t.Fatalf("指标改善应恢复正常: %+v", st)
	}
	if *recovers != 1 {
		t.Fatalf("应触发一次 OnRecover: %d", *recovers)
	}
}

func TestMonitorMetricQueryErrorAbstainsR2(t *testing.T) {
	ctx := context.Background()
	port := newFakePort()
	m, stalls, _ := newTestMonitor(port)

	now := time.Now()
	last := now.Add(-2 * time.Minute)
	port.sessions = []model.StallSessionInfo{{ID: 5, StartedAt: now.Add(-30 * time.Minute)}}
	port.lastAct[5] = &last
	port.actCount[5] = 3
	port.winErr[5] = true // Prometheus 查询失败

	m.EvaluateNow(ctx)
	st, ok := m.Status(5)
	if !ok || st.State != model.StallStateNormal {
		t.Fatalf("指标查询失败时 R2 必须弃权，不误报: %+v", st)
	}
	if st.Evidence.HasTrend || st.Evidence.TrendSkippedReason != "指标查询失败" {
		t.Fatalf("应标注查询失败: %+v", st.Evidence)
	}
	if *stalls != 0 {
		t.Fatal("弃权时不应触发卡顿钩子")
	}
}

func TestMonitorListErrorKeepsOldState(t *testing.T) {
	ctx := context.Background()
	port := newFakePort()
	m, _, _ := newTestMonitor(port)

	port.sessions = []model.StallSessionInfo{{ID: 2, StartedAt: time.Now().Add(-8 * time.Minute)}}
	m.EvaluateNow(ctx)
	if st, ok := m.Status(2); !ok || st.State != model.StallStateStalled {
		t.Fatalf("首轮应卡顿: %+v", st)
	}

	// 列表查询失败：本轮跳过，旧状态保留，也不被误清理。
	port.listErr = errors.New("db down")
	m.EvaluateNow(ctx)
	if st, ok := m.Status(2); !ok || st.State != model.StallStateStalled {
		t.Fatalf("列表失败时应保留旧状态: ok=%v st=%+v", ok, st)
	}
}

func TestMonitorActivityErrorKeepsOldState(t *testing.T) {
	ctx := context.Background()
	port := newFakePort()
	m, stalls := func() (*StallMonitor, *int) {
		mm, s, _ := newTestMonitor(port)
		return mm, s
	}()

	port.sessions = []model.StallSessionInfo{{ID: 3, StartedAt: time.Now().Add(-8 * time.Minute)}}
	port.actErr[3] = true // 活动取数失败 → 本轮跳过该会话
	m.EvaluateNow(ctx)
	if _, ok := m.Status(3); ok {
		t.Fatal("取数失败的会话本轮不应写入状态")
	}
	if *stalls != 0 {
		t.Fatal("取数失败不应触发卡顿钩子")
	}
}

// abandonPort 构造一个连续空闲 timeout 的 running 会话。
func abandonPort(id int64, timeout time.Duration) *fakePort {
	port := newFakePort()
	now := time.Now()
	last := now.Add(-timeout)
	port.sessions = []model.StallSessionInfo{{ID: id, UserID: 7, ProjectID: 3, StartedAt: now.Add(-timeout - time.Minute)}}
	port.lastAct[id] = &last
	return port
}

func TestMonitorAbandonFiresOnce(t *testing.T) {
	ctx := context.Background()
	abandonAt := model.StallConfig{}.Normalize().AbandonTimeout
	port := abandonPort(11, abandonAt+time.Minute)
	m := NewStallMonitor(NewStallDetector(model.StallConfig{}), port, time.Second)

	var hits []model.StallSessionInfo
	m.OnAbandon(func(_ context.Context, info model.StallSessionInfo, _ model.StallStatus) error {
		hits = append(hits, info)
		return nil
	})

	m.EvaluateNow(ctx)
	if st, ok := m.Status(11); !ok || !st.Abandoned || st.Reason != model.StallReasonAbandoned {
		t.Fatalf("连续空闲超过 AbandonTimeout 应判弃班: ok=%v st=%+v", ok, st)
	}
	if len(hits) != 1 {
		t.Fatalf("首轮应触发一次弃班回调: %d", len(hits))
	}
	if hits[0].UserID != 7 || hits[0].ProjectID != 3 {
		t.Fatalf("回调应带上会话归属信息: %+v", hits[0])
	}

	// 会话仍是 running（钩子侧置终态有延迟）时不得重复回调，避免重复回收沙箱。
	m.EvaluateNow(ctx)
	m.EvaluateNow(ctx)
	if len(hits) != 1 {
		t.Fatalf("同一会话只应成功回调一次: %d", len(hits))
	}
}

func TestMonitorAbandonRetriesAfterFailure(t *testing.T) {
	ctx := context.Background()
	abandonAt := model.StallConfig{}.Normalize().AbandonTimeout
	port := abandonPort(12, abandonAt+time.Minute)
	m := NewStallMonitor(NewStallDetector(model.StallConfig{}), port, time.Second)

	var hits int
	fail := true
	m.OnAbandon(func(context.Context, model.StallSessionInfo, model.StallStatus) error {
		hits++
		if fail {
			return errors.New("db down")
		}
		return nil
	})

	m.EvaluateNow(ctx)
	if hits != 1 {
		t.Fatalf("失败回调也应尝试过一次: %d", hits)
	}

	// 上一轮失败：去重标记应被撤销，下一轮重试。
	fail = false
	m.EvaluateNow(ctx)
	if hits != 2 {
		t.Fatalf("失败后下一轮应重试: %d", hits)
	}

	// 成功后会话离开 running 列表：内存状态与去重标记一并清理。
	fail = true
	port.sessions = nil
	m.EvaluateNow(ctx)
	if hits != 2 {
		t.Fatalf("会话消失后不应再回调: %d", hits)
	}
	if _, ok := m.Status(12); ok {
		t.Fatal("会话离开 running 后状态应被清理")
	}
}

func TestMonitorNotAbandonedBelowTimeout(t *testing.T) {
	ctx := context.Background()
	cfg := model.StallConfig{}.Normalize()
	port := abandonPort(13, cfg.IdleTimeout+time.Minute) // 已过卡顿线、未过弃班线
	m := NewStallMonitor(NewStallDetector(model.StallConfig{}), port, time.Second)

	hits := 0
	m.OnAbandon(func(context.Context, model.StallSessionInfo, model.StallStatus) error {
		hits++
		return nil
	})

	m.EvaluateNow(ctx)
	st, ok := m.Status(13)
	if !ok || st.Reason != model.StallReasonIdle || st.Abandoned {
		t.Fatalf("未达弃班线只应判 idle: ok=%v st=%+v", ok, st)
	}
	if hits != 0 {
		t.Fatal("未达弃班线不应触发回调")
	}
}

func TestMonitorListStatusesSorted(t *testing.T) {
	ctx := context.Background()
	port := newFakePort()
	m, _, _ := newTestMonitor(port)

	port.sessions = []model.StallSessionInfo{
		{ID: 30, StartedAt: time.Now().Add(-8 * time.Minute)},
		{ID: 10, StartedAt: time.Now().Add(-8 * time.Minute)},
		{ID: 20, StartedAt: time.Now().Add(-8 * time.Minute)},
	}
	m.EvaluateNow(ctx)
	all := m.ListStatuses()
	if len(all) != 3 || all[0].SessionID != 10 || all[1].SessionID != 20 || all[2].SessionID != 30 {
		t.Fatalf("状态列表应按会话 ID 升序: %+v", all)
	}
}

func TestMonitorStopIdempotent(t *testing.T) {
	m, _, _ := newTestMonitor(newFakePort())
	m.Stop()
	m.Stop() // 重复 Stop 不得 panic（close of closed channel）
}
