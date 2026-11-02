// 人机对照数据统计模型（T3-26）。
//
// 基于 T3-25 交叉分组产生的配对会话，计算 AI 模式 vs 人工模式的四项差异：
//   - 速度差（耗时差异百分比）
//   - 正确率差（故障修复率差异）
//   - 危险操作率差（人均危险操作数差异）
//   - 验证行为差（验证率差异）
//
// 同时提供样本量校验，数据不足时明确提示，避免误导决策。
package model

import "time"

// AISide AI 模式的一侧数据。
type AISide struct {
	SessionID    int64   `json:"session_id"`
	DurationSec  int     `json:"duration_sec"`  // 耗时（秒）
	Resolved     bool    `json:"resolved"`      // 是否修复
	TechScore    float64 `json:"tech_score"`    // 技术分
	CostPenalty  float64 `json:"cost_penalty"`  // 操作代价惩罚分
	VerifyCount  int     `json:"verify_count"`  // 验证行为次数
	DangerousOps int     `json:"dangerous_ops"` // 危险操作次数
	HintCount    int     `json:"hint_count"`    // 使用提示次数
}

// HumanSide 人工模式的一侧数据。
type HumanSide struct {
	SessionID    int64   `json:"session_id"`
	DurationSec  int     `json:"duration_sec"`
	Resolved     bool    `json:"resolved"`
	TechScore    float64 `json:"tech_score"`
	CostPenalty  float64 `json:"cost_penalty"`
	VerifyCount  int     `json:"verify_count"`
	DangerousOps int     `json:"dangerous_ops"`
	HintCount    int     `json:"hint_count"`
}

// ABPair 一对对照会话的完整数据。
type ABPair struct {
	UserID     int64  `json:"user_id"`
	ProjectID  int64  `json:"project_id"`
	Category   string `json:"category"`   // 故障类目（同类同难度校准）
	Difficulty string `json:"difficulty"` // 难度
	Group      string `json:"group"`      // A 或 B（记录分组，用于分析顺序效应）

	AI    AISide    `json:"ai"`
	Human HumanSide `json:"human"`

	// 配对完成时间（以第二场完成时间为准）
	CompletedAt time.Time `json:"completed_at"`
}

// ABSummary 对照统计汇总结果。
type ABSummary struct {
	PairCount int `json:"pair_count"` // 有效对照组数

	// —— 四项核心差异（AI - 人工）——
	// 速度差：负数=AI更快，正数=AI更慢
	SpeedDiffPct float64 `json:"speed_diff_pct"`
	// 正确率差：正数=AI更高
	AccuracyDiffPct float64 `json:"accuracy_diff_pct"`
	// 危险操作率差：负数=AI更少（更好）
	DangerousOpDiffPct float64 `json:"dangerous_op_diff_pct"`
	// 验证行为差：正数=AI更多
	VerifyDiffPct float64 `json:"verify_diff_pct"`

	// —— 分项均值（便于对比）——
	AvgAIDurationSec     float64 `json:"avg_ai_duration_sec"`
	AvgHumanDurationSec  float64 `json:"avg_human_duration_sec"`
	AIAccuracyPct        float64 `json:"ai_accuracy_pct"`
	HumanAccuracyPct     float64 `json:"human_accuracy_pct"`
	AvgAIDangerousOps    float64 `json:"avg_ai_dangerous_ops"`
	AvgHumanDangerousOps float64 `json:"avg_human_dangerous_ops"`
	AvgAIVerifyCount     float64 `json:"avg_ai_verify_count"`
	AvgHumanVerifyCount  float64 `json:"avg_human_verify_count"`

	// 样本量信息
	SampleInfo SampleInfo `json:"sample_info"`
}

// SampleInfo 样本量信息与提示。
type SampleInfo struct {
	PairCount       int    `json:"pair_count"`       // 对照组数
	UserCount       int    `json:"user_count"`       // 参与用户数
	EnoughSample    bool   `json:"enough_sample"`    // 样本是否足够（≥30组）
	Tip             string `json:"tip"`              // 提示文案
	ConfidenceLevel string `json:"confidence_level"` // 置信度：高/中/低
}

// ProjectABStat 项目维度对照统计。
type ProjectABStat struct {
	ProjectID       int64   `json:"project_id"`
	ProjectName     string  `json:"project_name"`
	PairCount       int     `json:"pair_count"`
	SpeedDiffPct    float64 `json:"speed_diff_pct"`
	AccuracyDiffPct float64 `json:"accuracy_diff_pct"`
}

// ABFilter 对照统计筛选条件。
type ABFilter struct {
	ProjectID  int64  // 项目ID，0=不限
	Category   string // 故障类目，空=不限
	Difficulty string // 难度，空=不限
	StartDate  time.Time
	EndDate    time.Time
}
