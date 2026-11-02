// 卡顿检测纯规则核心（T3-6）。
//
// 本文件不含任何 PG / Prometheus / HTTP 依赖，输入输出都是普通值，
// 以便完整单测覆盖阈值与边界；仓储层与扫描器分别见 repository/stall_repo.go
// 与 stall_monitor.go。
package service

import (
	"time"

	"github.com/quenching/training-service/internal/model"
)

// StallDetector 卡顿规则评估器（无状态，可并发使用）。
type StallDetector struct {
	cfg model.StallConfig
}

// NewStallDetector 创建检测器，缺省阈值自动补默认值。
func NewStallDetector(cfg model.StallConfig) *StallDetector {
	return &StallDetector{cfg: cfg.Normalize()}
}

// Config 返回生效配置（已归一）。
func (d *StallDetector) Config() model.StallConfig { return d.cfg }

// EvaluateIdle 只执行空闲时长判定（R1/R3 同一时间轴），不依赖指标。
// 扫描器可先调它：命中空闲就不必再查 Prometheus，直接评估。
// 返回距最近活动经过秒数与是否构成空闲卡顿（R3 的弃班线由 Evaluate 细分）。
func (d *StallDetector) EvaluateIdle(in model.StallInput) (int, bool) {
	idleSec := idleSeconds(in)
	if idleSec < 0 {
		// 时间戳全缺或在未来：无法判定 R1，返回 -1 交调用方按"不超时"处理
		// （与 Evaluate 主路径 idleSec<0 时不置证据、不误判的口径一致）。
		return -1, false
	}
	return idleSec, time.Duration(idleSec)*time.Second >= d.cfg.IdleTimeout
}

// Evaluate 执行完整规则评估（R1/R3 优先；空闲未命中再评估 R2）。
func (d *StallDetector) Evaluate(sessionID int64, in model.StallInput) model.StallStatus {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	st := model.StallStatus{SessionID: sessionID, State: model.StallStateNormal, EvaluatedAt: now}

	idleSec := idleSeconds(in)
	if idleSec >= 0 {
		st.Evidence.IdleSeconds = idleSec
		idle := time.Duration(idleSec) * time.Second
		if idle >= d.cfg.IdleTimeout {
			st.State = model.StallStateStalled
			// R3：空闲已到弃班线，原因直接记 abandoned（必然同时满足 R1）。
			if idle >= d.cfg.AbandonTimeout {
				st.Reason = model.StallReasonAbandoned
				st.Abandoned = true
			} else {
				st.Reason = model.StallReasonIdle
			}
			st.Since = now
			return st
		}
	}
	st.Evidence.WindowActivities = in.WindowActivities

	// R2：窗口内操作不足表示用户没在尝试，不属于"有操作无进展"。
	if in.WindowActivities < d.cfg.MinActivities {
		return st
	}

	first, last, ok, skipReason := d.trendMeans(in)
	st.Evidence.HasTrend = ok
	st.Evidence.TrendSkippedReason = skipReason
	if !ok {
		// 保守原则：指标数据不足时 R2 弃权，绝不凭"查不到数据"误判卡顿。
		return st
	}
	st.Evidence.ErrorRateFirst = first.errorRate
	st.Evidence.ErrorRateLast = last.errorRate
	st.Evidence.P99First = first.p99
	st.Evidence.P99Last = last.p99
	st.Evidence.QPSFirst = first.qps
	st.Evidence.QPSLast = last.qps

	if d.improved(first, last) {
		return st // 任一业务指标朝改善方向变化，视为有进展。
	}

	st.State = model.StallStateStalled
	st.Reason = model.StallReasonNoProgress
	st.Since = now
	return st
}

// trendMean 三项业务指标的窗口首/末段均值。
type trendMean struct {
	errorRate float64
	p99       float64
	qps       float64
}

// trendMeans 计算三项指标窗口首段与末段均值。
// 三项必须全部"可取且点数足够"才返回 ok=true；任一不足即整体弃权并给出原因，
// 保证不会因为单个业务指标缺失（如服务暂无流量导致 QPS 无点）而误判无进展。
func (d *StallDetector) trendMeans(in model.StallInput) (trendMean, trendMean, bool, string) {
	erFirst, erLast, erOK := segmentMeans(in.ErrorRate, d.cfg.MinSegmentPoints)
	p99First, p99Last, p99OK := segmentMeans(in.P99Latency, d.cfg.MinSegmentPoints)
	qpsFirst, qpsLast, qpsOK := segmentMeans(in.QPS, d.cfg.MinSegmentPoints)

	if !in.ErrorRate.Available || !in.P99Latency.Available || !in.QPS.Available {
		return trendMean{}, trendMean{}, false, "指标查询失败"
	}
	if !erOK || !p99OK || !qpsOK {
		return trendMean{}, trendMean{}, false, "指标点不足"
	}
	return trendMean{errorRate: erFirst, p99: p99First, qps: qpsFirst},
		trendMean{errorRate: erLast, p99: p99Last, qps: qpsLast},
		true, ""
}

// improved 判断末段相对首段是否出现改善方向（任一指标改善即算有进展）。
//   - 错误率：下降 >= MinErrorRateDrop 个百分点；
//   - P99：下降 >= MinP99Drop 秒；
//   - QPS：回升同时满足相对比例与绝对增量门槛（首段为 0 时只看绝对增量）。
func (d *StallDetector) improved(first, last trendMean) bool {
	if first.errorRate-last.errorRate >= d.cfg.MinErrorRateDrop {
		return true
	}
	if first.p99-last.p99 >= d.cfg.MinP99Drop {
		return true
	}
	qpsGain := last.qps - first.qps
	if qpsGain < d.cfg.MinQPSAbsoluteGain {
		return false
	}
	if first.qps <= 0 {
		return true // 低基数/零基数时以绝对增量为准。
	}
	return qpsGain/first.qps >= d.cfg.MinQPSRelativeGain
}

// idleSeconds 计算距最近有效活动的经过秒数；无活动记录时以 started_at 兜底；
// 两者都缺失返回 -1（无法判定，交调用方按"不超时"处理）。
func idleSeconds(in model.StallInput) int {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	last := in.LastActivityAt
	if last.IsZero() {
		last = in.StartedAt
	}
	if last.IsZero() || last.After(now) {
		return -1
	}
	return int(now.Sub(last).Seconds())
}

// segmentMeans 将窗口数据点按时间顺序等分为首/末两段（奇数点中间点归首段），
// 各段点数不足 minPoints 时返回 ok=false。入参假定已按时间升序（仓储层保证）。
func segmentMeans(w model.StallMetricWindow, minPoints int) (float64, float64, bool) {
	n := len(w.Points)
	minTotal := 2 * minPoints
	if n < minTotal {
		return 0, 0, false
	}
	mid := n / 2
	if n%2 == 1 {
		mid++ // 奇数点：首段多一个，保证两段都至少 minPoints（n>=2*minPoints 时成立）
	}
	return avgPoints(w.Points[:mid]), avgPoints(w.Points[mid:]), true
}

// avgPoints 计算数据点均值；空切片返回 0。
func avgPoints(ps []model.StallMetricPoint) float64 {
	if len(ps) == 0 {
		return 0
	}
	var sum float64
	for _, p := range ps {
		sum += p.Value
	}
	return sum / float64(len(ps))
}
