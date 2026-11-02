// 卡顿检测器纯规则单测（T3-6）。
//
// 全部用例只构造 model.StallInput 值，不依赖 PG / Prometheus / 时钟注入以外的副作用，
// 覆盖：R1 空闲命中与边界、R3 弃班命中与边界、活动抑制、R2 无进展命中、三指标任一改善解除、
// 数据不足保守弃权、时间兜底、奇数点切分、QPS 零基数口径。
package service

import (
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// testProgressWindow 归一化后的默认观察窗口，从 model.StallConfig 派生，
// 不在测试内复制 10 分钟字面值（默认常量为 model 包内未导出名）。
var testProgressWindow = model.StallConfig{}.Normalize().ProgressWindow

// points 快速构造 n 个等间隔（15s）指标点，值为 vals（不足按末值补齐）。
func points(start time.Time, vals ...float64) []model.StallMetricPoint {
	ps := make([]model.StallMetricPoint, 0, len(vals))
	for i, v := range vals {
		ps = append(ps, model.StallMetricPoint{
			TimestampMS: start.Add(time.Duration(i) * 15 * time.Second).UnixMilli(),
			Value:       v,
		})
	}
	return ps
}

// fullTrend 构造三指标均可用、各 4 个点的窗口；首段为前 2 点均值，末段为后 2 点均值。
func fullTrend(start time.Time, er, p99, qps []float64) (model.StallMetricWindow, model.StallMetricWindow, model.StallMetricWindow) {
	win := func(vals []float64) model.StallMetricWindow {
		return model.StallMetricWindow{Available: true, Points: points(start, vals...)}
	}
	return win(er), win(p99), win(qps)
}

func TestR1IdleHit(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// 最近活动在 6 分钟前，超过 5 分钟阈值。
	in := model.StallInput{
		Now:            now,
		LastActivityAt: now.Add(-6 * time.Minute),
	}
	got := d.Evaluate(101, in)
	if got.State != model.StallStateStalled || got.Reason != model.StallReasonIdle {
		t.Fatalf("应判空闲卡顿: state=%s reason=%s", got.State, got.Reason)
	}
	if got.Evidence.IdleSeconds != 360 {
		t.Fatalf("空闲秒数错误: %d", got.Evidence.IdleSeconds)
	}
	if !got.Since.Equal(now) {
		t.Fatalf("Since 应为评估时刻")
	}

	// EvaluateIdle 快路径应与之一致，且无需指标。
	sec, idle := d.EvaluateIdle(in)
	if !idle || sec != 360 {
		t.Fatalf("EvaluateIdle 快路径结果错误: sec=%d idle=%v", sec, idle)
	}
}

func TestR1IdleBoundary(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()

	// 恰好 5 分钟（300s）视为达到阈值。
	sec, idle := d.EvaluateIdle(model.StallInput{Now: now, LastActivityAt: now.Add(-5 * time.Minute)})
	if !idle || sec != 300 {
		t.Fatalf("恰好 5 分钟应命中: sec=%d idle=%v", sec, idle)
	}

	// 4 分 59 秒不命中。
	_, idle = d.EvaluateIdle(model.StallInput{Now: now, LastActivityAt: now.Add(-(5*time.Minute - time.Second))})
	if idle {
		t.Fatal("4分59秒不应判空闲")
	}
}

func TestR1NoActivityFallbackToStartedAt(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()

	// 从无活动（LastActivityAt 零值），以 started_at 起算：8 分钟前开训 → 空闲。
	got := d.Evaluate(1, model.StallInput{Now: now, StartedAt: now.Add(-8 * time.Minute)})
	if got.State != model.StallStateStalled || got.Reason != model.StallReasonIdle {
		t.Fatalf("无活动时应以 started_at 兜底判空闲: %+v", got)
	}

	// 刚开训 1 分钟且无活动 → 不空闲；活动数也不足 → 正常。
	got = d.Evaluate(1, model.StallInput{Now: now, StartedAt: now.Add(-time.Minute)})
	if got.State != model.StallStateNormal {
		t.Fatalf("开训 1 分钟不应卡顿: %+v", got)
	}
}

func TestR1IndeterminateTimes(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()

	// started_at 与 last_activity 都缺失：无法判 R1；活动数不足 → 正常，不误判。
	got := d.Evaluate(1, model.StallInput{Now: now})
	if got.State != model.StallStateNormal || got.Evidence.IdleSeconds != 0 {
		t.Fatalf("时间全缺应弃权 R1: %+v", got)
	}
	if sec, idle := d.EvaluateIdle(model.StallInput{Now: now}); sec != -1 || idle {
		t.Fatalf("时间全缺快路径应返回 -1/false: sec=%d", sec)
	}

	// 活动时间在未来（时钟漂移/脏数据）：不判空闲。
	got = d.Evaluate(1, model.StallInput{Now: now, LastActivityAt: now.Add(time.Minute)})
	if got.State != model.StallStateNormal {
		t.Fatalf("未来活动时间不应判卡顿: %+v", got)
	}
}

func TestR3AbandonHit(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	defaults := model.StallConfig{}.Normalize()
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// 空闲恰好达到弃班线：判弃班（不再记 idle），Abandoned 置位供扫描器触发回收。
	got := d.Evaluate(1, model.StallInput{Now: now, LastActivityAt: now.Add(-defaults.AbandonTimeout)})
	if got.State != model.StallStateStalled || got.Reason != model.StallReasonAbandoned || !got.Abandoned {
		t.Fatalf("达到弃班线应判 abandoned: %+v", got)
	}

	// 差 1 秒未到弃班线：只判空闲卡顿，不置 Abandoned。
	got = d.Evaluate(1, model.StallInput{Now: now,
		LastActivityAt: now.Add(-defaults.AbandonTimeout + time.Second)})
	if got.Reason != model.StallReasonIdle || got.Abandoned {
		t.Fatalf("未达弃班线只应判 idle: %+v", got)
	}

	// 无活动时以 started_at 起算同样能命中弃班。
	got = d.Evaluate(1, model.StallInput{Now: now,
		StartedAt: now.Add(-defaults.AbandonTimeout - time.Minute)})
	if !got.Abandoned {
		t.Fatalf("开训后从无活动也应能判弃班: %+v", got)
	}

	// 时间不可判（全缺）时不得误判弃班。
	if got = d.Evaluate(1, model.StallInput{Now: now}); got.Abandoned {
		t.Fatalf("时间全缺不应判弃班: %+v", got)
	}
}

func TestR2HitNoProgress(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	windowStart := now.Add(-testProgressWindow)

	// 最近活动 2 分钟前（R1 不命中），窗口内 3 次活动；三指标首末段完全持平。
	er, p99, qps := fullTrend(windowStart,
		[]float64{5, 5, 5, 5},
		[]float64{0.5, 0.5, 0.5, 0.5},
		[]float64{10, 10, 10, 10},
	)
	in := model.StallInput{
		Now:              now,
		StartedAt:        now.Add(-20 * time.Minute),
		LastActivityAt:   now.Add(-2 * time.Minute),
		WindowActivities: 3,
		ErrorRate:        er,
		P99Latency:       p99,
		QPS:              qps,
	}
	got := d.Evaluate(7, in)
	if got.State != model.StallStateStalled || got.Reason != model.StallReasonNoProgress {
		t.Fatalf("有活动无指标改善应判无进展卡顿: %+v", got)
	}
	if !got.Evidence.HasTrend {
		t.Fatal("应携带趋势证据")
	}
	if got.Evidence.ErrorRateFirst != 5 || got.Evidence.ErrorRateLast != 5 {
		t.Fatalf("错误率段均值错误: first=%.2f last=%.2f",
			got.Evidence.ErrorRateFirst, got.Evidence.ErrorRateLast)
	}
}

func TestR2RelievedByEachMetric(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()
	ws := now.Add(-testProgressWindow)

	cases := []struct {
		name string
		er   []float64
		p99  []float64
		qps  []float64
	}{
		{
			name: "错误率下降1.5pp",
			er:   []float64{5, 5, 3.5, 3.5},
			p99:  []float64{0.5, 0.5, 0.5, 0.5},
			qps:  []float64{10, 10, 10, 10},
		},
		{
			name: "P99下降100ms",
			er:   []float64{5, 5, 5, 5},
			p99:  []float64{0.5, 0.5, 0.4, 0.4},
			qps:  []float64{10, 10, 10, 10},
		},
		{
			name: "QPS从10回升到13",
			er:   []float64{5, 5, 5, 5},
			p99:  []float64{0.5, 0.5, 0.5, 0.5},
			qps:  []float64{10, 10, 13, 13},
		},
		{
			name: "QPS零基数绝对回升2",
			er:   []float64{5, 5, 5, 5},
			p99:  []float64{0.5, 0.5, 0.5, 0.5},
			qps:  []float64{0, 0, 2, 2},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			er, p99, qw := fullTrend(ws, tc.er, tc.p99, tc.qps)
			in := model.StallInput{
				Now:              now,
				StartedAt:        now.Add(-20 * time.Minute),
				LastActivityAt:   now.Add(-2 * time.Minute),
				WindowActivities: 2,
				ErrorRate:        er,
				P99Latency:       p99,
				QPS:              qw,
			}
			if got := d.Evaluate(1, in); got.State != model.StallStateNormal {
				t.Fatalf("出现改善方向应解除卡顿: %+v", got)
			}
		})
	}
}

func TestR2ImprovementBelowThresholdStillStalled(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()
	ws := now.Add(-testProgressWindow)

	// 三项都有朝好方向的微小变化，但都未达改善门槛 → 仍判无进展。
	er, p99, qw := fullTrend(ws,
		[]float64{5, 5, 4.5, 4.5},         // 仅降 0.5pp，门槛 1pp
		[]float64{0.5, 0.5, 0.49, 0.49},   // 仅降 10ms，门槛 50ms
		[]float64{100, 100, 100.5, 100.5}, // 绝对增量 0.5 < 1，且远低于 10%
	)
	in := model.StallInput{
		Now:              now,
		StartedAt:        now.Add(-20 * time.Minute),
		LastActivityAt:   now.Add(-2 * time.Minute),
		WindowActivities: 2,
		ErrorRate:        er,
		P99Latency:       p99,
		QPS:              qw,
	}
	if got := d.Evaluate(1, in); got.State != model.StallStateStalled || got.Reason != model.StallReasonNoProgress {
		t.Fatalf("改善不达门槛仍应判无进展: %+v", got)
	}
}

func TestR2QPSRelativeOnlyNotEnoughAtLowBase(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()
	ws := now.Add(-testProgressWindow)

	// 低基数：2 → 3，相对 +50% 满足，但绝对增量 1 恰好达到门槛（>=1 通过）。
	// 再取 2 → 2.5：相对 +25% 满足，但绝对增量 0.5 < 1 → 不改善。
	er, p99, qw := fullTrend(ws,
		[]float64{5, 5, 5, 5},
		[]float64{0.5, 0.5, 0.5, 0.5},
		[]float64{2, 2, 2.5, 2.5},
	)
	in := model.StallInput{
		Now:              now,
		StartedAt:        now.Add(-20 * time.Minute),
		LastActivityAt:   now.Add(-2 * time.Minute),
		WindowActivities: 2,
		ErrorRate:        er,
		P99Latency:       p99,
		QPS:              qw,
	}
	if got := d.Evaluate(1, in); got.State != model.StallStateStalled {
		t.Fatalf("低基数下绝对增量不足不应算改善: %+v", got)
	}
}

func TestR2SkipsWhenActivitiesInsufficient(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()
	ws := now.Add(-testProgressWindow)

	// 窗口内仅 1 次活动（门槛 2）：即便指标持平也不进入 R2，直接正常。
	er, p99, qw := fullTrend(ws,
		[]float64{5, 5, 5, 5},
		[]float64{0.5, 0.5, 0.5, 0.5},
		[]float64{10, 10, 10, 10},
	)
	in := model.StallInput{
		Now:              now,
		StartedAt:        now.Add(-20 * time.Minute),
		LastActivityAt:   now.Add(-2 * time.Minute),
		WindowActivities: 1,
		ErrorRate:        er,
		P99Latency:       p99,
		QPS:              qw,
	}
	got := d.Evaluate(1, in)
	if got.State != model.StallStateNormal {
		t.Fatalf("活动不足不应进入 R2: %+v", got)
	}
	if got.Evidence.HasTrend {
		t.Fatal("活动不足时不应计算趋势")
	}
}

func TestR2AbstainsWhenMetricUnavailable(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()
	ws := now.Add(-testProgressWindow)
	avail := model.StallMetricWindow{Available: true, Points: points(ws, 5, 5, 5, 5)}
	down := model.StallMetricWindow{Available: false} // 模拟 Prometheus 查询失败

	in := model.StallInput{
		Now:              now,
		StartedAt:        now.Add(-20 * time.Minute),
		LastActivityAt:   now.Add(-2 * time.Minute),
		WindowActivities: 3,
		ErrorRate:        avail,
		P99Latency:       avail,
		QPS:              down,
	}
	got := d.Evaluate(1, in)
	if got.State != model.StallStateNormal {
		t.Fatalf("指标不可用时 R2 必须弃权，不得误报: %+v", got)
	}
	if got.Evidence.HasTrend || got.Evidence.TrendSkippedReason != "指标查询失败" {
		t.Fatalf("应标注查询失败弃权: %+v", got.Evidence)
	}
}

func TestR2AbstainsWhenPointsInsufficient(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	now := time.Now()
	ws := now.Add(-testProgressWindow)

	// 三指标可用但只有 3 个点（需要 >=4）。
	few := model.StallMetricWindow{Available: true, Points: points(ws, 5, 5, 5)}
	in := model.StallInput{
		Now:              now,
		StartedAt:        now.Add(-20 * time.Minute),
		LastActivityAt:   now.Add(-2 * time.Minute),
		WindowActivities: 3,
		ErrorRate:        few,
		P99Latency:       few,
		QPS:              few,
	}
	got := d.Evaluate(1, in)
	if got.State != model.StallStateNormal {
		t.Fatalf("指标点不足时 R2 必须弃权: %+v", got)
	}
	if got.Evidence.HasTrend || got.Evidence.TrendSkippedReason != "指标点不足" {
		t.Fatalf("应标注点不足弃权: %+v", got.Evidence)
	}
}

func TestSegmentMeansOddPoints(t *testing.T) {
	// 5 个点（奇数）：中间点归首段 → 首段 3 点、末段 2 点。
	w := model.StallMetricWindow{Points: points(time.Now(), 2, 4, 6, 8, 10)}
	first, last, ok := segmentMeans(w, 2)
	if !ok {
		t.Fatal("5 个点应足以切分（首段3末段2）")
	}
	if first != 4 { // (2+4+6)/3
		t.Fatalf("首段均值错误: %.2f", first)
	}
	if last != 9 { // (8+10)/2
		t.Fatalf("末段均值错误: %.2f", last)
	}

	// 3 个点、minPoints=2 时总数不足（需要 4），弃权。
	if _, _, ok := segmentMeans(model.StallMetricWindow{Points: points(time.Now(), 1, 2, 3)}, 2); ok {
		t.Fatal("3 个点不满足首末段各 2 点，应返回 false")
	}

	// minPoints=1 时 3 点切分为 2/1。
	first, last, ok = segmentMeans(model.StallMetricWindow{Points: points(time.Now(), 2, 4, 8)}, 1)
	if !ok || first != 3 || last != 8 {
		t.Fatalf("minPoints=1 奇数点切分错误: first=%.2f last=%.2f ok=%v", first, last, ok)
	}
}

func TestZeroNowUsesWallClock(t *testing.T) {
	d := NewStallDetector(model.StallConfig{})
	// 不传 Now：以 time.Now() 为准；活动在很久以前 → 空闲。
	got := d.Evaluate(1, model.StallInput{LastActivityAt: time.Now().Add(-10 * time.Minute)})
	if got.State != model.StallStateStalled || got.Reason != model.StallReasonIdle {
		t.Fatalf("零值 Now 应取当前时间并判空闲: %+v", got)
	}
	if got.EvaluatedAt.IsZero() {
		t.Fatal("EvaluatedAt 应被填充")
	}
}
