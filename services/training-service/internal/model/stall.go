// 训练会话卡顿检测模型（T3-6）。
//
// 只负责"识别卡顿状态"：对 running 中的训练会话，按纯规则判定用户是否卡住，
// 状态保存在内存中，供后续 T3-7 主动线索推送复用。规则核心不做推送、不做频控、
// 不落库（状态可从 PG/Prometheus 随时重算，进程重启后下一轮评估自动恢复）。
//
// 三条规则（任一命中即卡顿，仅评估 running 会话）：
//
//	R1 空闲超时：距最近一次有效活动超过 IdleTimeout（默认 5 分钟）。
//	           有效活动 = session_operations 任意操作，或 ai_messages 中用户主动提问；
//	           开训后从无活动时以 started_at 起算。
//	R2 有操作无进展：ProgressWindow（默认 10 分钟）内有效活动 >= MinActivities 次，
//	           但 error_rate / p99_latency / qps 三项业务指标均无改善方向
//	           （数据不足时该规则保守跳过，避免误报）。
//	R3 弃班：连续空闲超过 AbandonTimeout（默认 20 分钟），本次班次判定未完成情况，
//	           由扫描器一次性触发回收（R3 必然同时满足 R1，故 Reason 直接记 abandoned）。
package model

import "time"

// StallState 卡顿状态。
type StallState string

const (
	// StallStateNormal 正常（未检测到卡顿）。
	StallStateNormal StallState = "normal"
	// StallStateStalled 已卡顿。
	StallStateStalled StallState = "stalled"
)

// StallReason 卡顿原因（命中的规则）。
type StallReason string

const (
	// StallReasonNone 未卡顿。
	StallReasonNone StallReason = ""
	// StallReasonIdle R1：长时间无有效活动。
	StallReasonIdle StallReason = "idle"
	// StallReasonNoProgress R2：有操作但指标无改善方向。
	StallReasonNoProgress StallReason = "no_progress"
	// StallReasonAbandoned R3：连续空闲超过 AbandonTimeout，判定弃班。
	StallReasonAbandoned StallReason = "abandoned"
)

// StallConfig 检测阈值（全部可配，零值由 Normalize 填默认）。
type StallConfig struct {
	// IdleTimeout R1：距最近有效活动超过该时长判空闲卡顿。
	IdleTimeout time.Duration
	// AbandonTimeout R3：连续空闲超过该时长判弃班——本次班次到此为止，
	// 会话置 aborted、沙箱回收、复盘按"未完成"出。不小于 IdleTimeout。
	AbandonTimeout time.Duration
	// ProgressWindow R2：观察操作与指标变化的时间窗口。
	ProgressWindow time.Duration
	// MinActivities R2：窗口内至少多少次有效活动才参与"无进展"判定。
	MinActivities int
	// MinSegmentPoints R2：窗口首/末段至少各有多少个指标点才做趋势比较。
	MinSegmentPoints int
	// MinErrorRateDrop R2：错误率（百分点，0-100）下降至少该值算改善。
	MinErrorRateDrop float64
	// MinP99Drop R2：P99 延迟（秒）下降至少该值算改善。
	MinP99Drop float64
	// MinQPSRelativeGain R2：QPS 相对回升比例（0.1 = 10%）算改善。
	MinQPSRelativeGain float64
	// MinQPSAbsoluteGain R2：QPS 绝对回升下限（次/秒），低基数时避免相对值过敏。
	MinQPSAbsoluteGain float64
}

// 默认阈值（配置缺省时使用，见 Normalize）。
const (
	defaultIdleTimeout      = 5 * time.Minute
	defaultAbandonTimeout   = 20 * time.Minute
	defaultProgressWindow   = 10 * time.Minute
	defaultMinActivities    = 2
	defaultMinSegmentPoints = 2
	defaultErrorRateDropPct = 1.0  // 错误率下降 1 个百分点
	defaultP99DropSec       = 0.05 // P99 下降 50ms
	defaultQPSRelativeGain  = 0.10 // QPS 回升 10%
	defaultQPSAbsoluteGain  = 1.0  // 且绝对增量至少 1 次/秒
)

// Normalize 填默认值并做下限保护，返回一份归一配置。
//
// 时长与计数项（IdleTimeout/ProgressWindow/MinActivities/MinSegmentPoints）
// 零值无业务意义，一律兜底为默认值；四项改善阈值的零值是合法显式配置
// （门槛 0 = 任意微小改善都算有进展），只有负值才判为误配并兜底。
// 区分"未配置"与"显式配 0"：阈值零值本身无法自证（StallConfig{} 与显式配 0
// 难以只靠阈值区分），故以时长/计数四项归一化前是否全零作为判据——
// 全零说明调用方什么都没配（StallConfig{}），阈值一并填默认；只要配了任一时长
// 或计数项，即视为一份有意给出的配置，零阈值按"门槛 0"原样保留。
func (c StallConfig) Normalize() StallConfig {
	unconfigured := c.IdleTimeout == 0 && c.ProgressWindow == 0 &&
		c.MinActivities == 0 && c.MinSegmentPoints == 0 && c.AbandonTimeout == 0
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = defaultIdleTimeout
	}
	if c.AbandonTimeout <= 0 {
		c.AbandonTimeout = defaultAbandonTimeout
	}
	// 弃班必须晚于卡顿判定，否则"弱引导"还没来得及推就被判结束。
	if c.AbandonTimeout < c.IdleTimeout {
		c.AbandonTimeout = c.IdleTimeout
	}
	if c.ProgressWindow <= 0 {
		c.ProgressWindow = defaultProgressWindow
	}
	if c.MinActivities <= 0 {
		c.MinActivities = defaultMinActivities
	}
	if c.MinSegmentPoints <= 0 {
		c.MinSegmentPoints = defaultMinSegmentPoints
	}
	if unconfigured {
		c.MinErrorRateDrop = defaultErrorRateDropPct
		c.MinP99Drop = defaultP99DropSec
		c.MinQPSRelativeGain = defaultQPSRelativeGain
		c.MinQPSAbsoluteGain = defaultQPSAbsoluteGain
		return c
	}
	if c.MinErrorRateDrop < 0 {
		c.MinErrorRateDrop = defaultErrorRateDropPct
	}
	if c.MinP99Drop < 0 {
		c.MinP99Drop = defaultP99DropSec
	}
	if c.MinQPSRelativeGain < 0 {
		c.MinQPSRelativeGain = defaultQPSRelativeGain
	}
	if c.MinQPSAbsoluteGain < 0 {
		c.MinQPSAbsoluteGain = defaultQPSAbsoluteGain
	}
	return c
}

// StallMetricWindow 单项指标在观察窗口内的时间序列（已按时间升序）。
type StallMetricWindow struct {
	// Available 指标是否可取。false 表示 Prometheus 不可用/查询失败，R2 对该项弃权。
	Available bool
	// Points 数据点（毫秒时间戳 + 值），升序。
	Points []StallMetricPoint
}

// StallMetricPoint 单个指标数据点。
type StallMetricPoint struct {
	TimestampMS int64
	Value       float64
}

// StallMetricWindows 三指标观察窗口（仓储层翻译自 metrics.Service）。
type StallMetricWindows struct {
	ErrorRate  StallMetricWindow
	P99Latency StallMetricWindow
	QPS        StallMetricWindow
}

// StallSessionInfo 运行中会话的最小信息（仓储层翻译自 training_sessions）。
type StallSessionInfo struct {
	// ID 会话 ID。
	ID int64
	// UserID 会话归属用户（弃班时触发生成复盘需要）。
	UserID int64
	// ProjectID 实训项目（复盘生成入参需要）。
	ProjectID int64
	// StartedAt 开训时间；未正式开训（started_at 为 NULL）时为零值。
	StartedAt time.Time
}

// StallInput 单会话单次评估的输入（已由仓储层翻译好，核心规则不碰 PG/Prometheus）。
type StallInput struct {
	// Now 评估时刻。
	Now time.Time
	// StartedAt 实训开始时间（开训后从无活动时用它兜底）。
	StartedAt time.Time
	// LastActivityAt 最近一次有效活动时间；零值表示从无活动。
	LastActivityAt time.Time
	// WindowActivities 观察窗口 [Now-ProgressWindow, Now] 内的有效活动次数。
	WindowActivities int
	// ErrorRate / P99Latency / QPS 三项业务指标窗口序列。
	ErrorRate  StallMetricWindow
	P99Latency StallMetricWindow
	QPS        StallMetricWindow
}

// StallEvidence 判定证据，便于云端验证与后续推送说明原因（不暴露内部计算细节给前端也无妨）。
type StallEvidence struct {
	// IdleSeconds 距最近有效活动已过去的秒数（无活动时相对 started_at）。
	IdleSeconds int `json:"idle_seconds"`
	// WindowActivities 观察窗口内有效活动次数。
	WindowActivities int `json:"window_activities"`
	// ErrorRateFirst / ErrorRateLast 错误率窗口首/末段均值；HasTrend=false 时无意义。
	ErrorRateFirst float64 `json:"error_rate_first,omitempty"`
	ErrorRateLast  float64 `json:"error_rate_last,omitempty"`
	// P99First / P99Last P99 延迟窗口首/末段均值（秒）。
	P99First float64 `json:"p99_first,omitempty"`
	P99Last  float64 `json:"p99_last,omitempty"`
	// QPSFirst / QPSLast QPS 窗口首/末段均值（次/秒）。
	QPSFirst float64 `json:"qps_first,omitempty"`
	QPSLast  float64 `json:"qps_last,omitempty"`
	// HasTrend R2 是否拿到了足以比较的指标数据。
	HasTrend bool `json:"has_trend"`
	// TrendSkippedReason 数据不足时的说明（如"指标点不足"）。
	TrendSkippedReason string `json:"trend_skipped_reason,omitempty"`
}

// StallStatus 单会话卡顿评估结果。
type StallStatus struct {
	SessionID int64       `json:"session_id"`
	State     StallState  `json:"state"`
	Reason    StallReason `json:"reason,omitempty"`
	// Since 进入当前状态的时间：卡顿时为首次判卡顿的评估时刻，正常时为零值。
	Since    time.Time     `json:"since,omitempty"`
	Evidence StallEvidence `json:"evidence"`
	// Abandoned R3 命中：连续空闲已超过 AbandonTimeout，应判弃班。
	// 扫描器据此触发一次性回调（会话置终止 + 回收沙箱 + 出未完成复盘）。
	Abandoned bool `json:"abandoned,omitempty"`
	// EvaluatedAt 本次评估时刻。
	EvaluatedAt time.Time `json:"evaluated_at"`
}
