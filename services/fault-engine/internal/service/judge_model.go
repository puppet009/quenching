// judge_model.go 故障修复判定模型。
// 规则：轮询受故障影响的指标，只有"所有指标都回到正常阈值内，且连续稳定一段时间"
// 才判定为已修复。能区分'表面恢复（瞬间达标又恶化）'与'真修复（持续达标）'。
// 指标来源抽象成 JudgeMetricFetcher（云上接 Prometheus，测试用 fake），判定逻辑纯函数可单测。
package service

import "time"

// JudgeOperator 阈值比较算子。
type JudgeOperator string

const (
	JudgeOpLTE JudgeOperator = "lte" // 指标值必须 <= 阈值（如错误率、CPU、延迟）
	JudgeOpGTE JudgeOperator = "gte" // 指标值必须 >= 阈值（如成功率、QPS）
)

// JudgeThreshold 单个指标的"正常"条件。故障剧本中定义：哪些指标、回到什么范围算正常。
type JudgeThreshold struct {
	Name    string        // 指标名，如 error_rate / cpu_usage / p99_latency
	Op      JudgeOperator // 比较方向
	Value   float64       // 阈值
	Service string        // 所属服务（多服务故障时区分）
}

// JudgeMetricSample 一次采样的指标值。
type JudgeMetricSample struct {
	Name    string
	Service string
	Value   float64
}

// JudgeVerdict 判定结论。
type JudgeVerdict string

const (
	JudgeVerdictUnknown     JudgeVerdict = "unknown"      // 数据不足/未开始
	JudgeVerdictNotResolved JudgeVerdict = "not_resolved" // 仍有指标异常
	JudgeVerdictResolved    JudgeVerdict = "resolved"     // 已确认修复（持续达标）
)

// JudgeResult 一次评估结果。
type JudgeResult struct {
	Verdict JudgeVerdict
	// Abnormal 当前仍异常的指标名（便于前端/日志说明为什么还没判定解决）。
	Abnormal []string
	// StableFor 已连续达标时长。
	StableFor time.Duration
	// StableRequired 需要连续达标多久才算真修复（稳定窗口）。
	StableRequired time.Duration
}
