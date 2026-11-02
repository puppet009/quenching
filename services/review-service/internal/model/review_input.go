// 复盘生成的输入数据（T2-29）：操作时间线 + 故障剧本 + 指标快照。
//
// 这些数据是 review-service 之外的属主表：
//   - session_operations / training_sessions 属 training-service（T1-23 时间线）；
//   - defects 属 fault-engine（T2-25/T2-26 故障剧本种子）；
//   - 指标快照未来由 sandbox-service 提供（MVP 监控未落 PG，端口预留）。
//
// MVP 共库部署，PG 适配器跨服务只读查询这些表；拆库后把 InputProvider
// 的实现换成 HTTP/gRPC 调用即可，领域模型与 Prompt 构建逻辑不动。

package model

import "encoding/json"

// TimelineOperation 时间线上的一次用户操作（对应 session_operations 一行）。
type TimelineOperation struct {
	// OffsetSec 相对实训开始的经过秒数（session_operations.virtual_timestamp）。
	OffsetSec int `json:"t"`
	// Type 操作类型：view_metric/view_log/exec_command/restart_service/...
	Type string `json:"type"`
	// Category 操作归类：observe/analyze/action/ai_assist。
	Category string `json:"category,omitempty"`
	// Target 操作对象（服务名/实例/指标名等）。
	Target string `json:"target,omitempty"`
	// Result 结果：executed/success/failed/skipped。
	Result string `json:"result,omitempty"`
}

// FaultScript 故障剧本摘要（defects 表 + session_defects 状态）。
type FaultScript struct {
	// DefectID 缺陷 ID（fault-engine.defects.id）。
	DefectID int64 `json:"defect_id"`
	// Code 缺陷代码，如 cpu-load-high。
	Code string `json:"code"`
	// Name 缺陷名称。
	Name string `json:"name"`
	// Category 故障类别：database/cache/mq/network/cpu/memory...
	Category string `json:"category"`
	// Difficulty 难度：easy/medium/hard。
	Difficulty string `json:"difficulty,omitempty"`
	// Description 故障描述（用户可见的现象）。
	Description string `json:"description,omitempty"`
	// RootCause 剧本预设根因（供 AI 对照，不强制照搬）。
	RootCause string `json:"root_cause,omitempty"`
	// Status 本次会话中该缺陷的状态：triggered/identified/fixed/timeout。
	Status string `json:"status"`
	// TimeToIdentify 识别耗时（秒），未识别为 0。
	TimeToIdentify int `json:"time_to_identify_sec,omitempty"`
	// TimeToFix 修复耗时（秒），未修复为 0。
	TimeToFix int `json:"time_to_fix_sec,omitempty"`
}

// MetricSnapshot 关键指标快照点。MVP 监控指标尚未落 PG，字段预留；
// 有数据时按时间序给 AI 描述指标拐点，无数据时 Prompt 中省略本节。
type MetricSnapshot struct {
	// OffsetSec 相对实训开始的经过秒数。
	OffsetSec int `json:"t"`
	// Name 指标名，如 cpu_percent / memory_percent / pod_abnormal。
	Name string `json:"name"`
	// Value 指标值。
	Value float64 `json:"value"`
	// Note 备注（如阈值、单位）。
	Note string `json:"note,omitempty"`
}

// ReviewInput 一次复盘生成所需的全部输入。
type ReviewInput struct {
	Brain     *BrainReviewInput   `json:"brain,omitempty"`
	SessionID int64               `json:"session_id"`
	FaultType string              `json:"fault_type"`
	Timeline  []TimelineOperation `json:"timeline"`
	Scripts   []FaultScript       `json:"scripts"`
	Metrics   []MetricSnapshot    `json:"metrics,omitempty"`
	// Difficulty 会话难度，来自 training_sessions.difficulty。
	Difficulty string `json:"difficulty,omitempty"`
}

// TimelineJSON 时间线序列化为 AI 输入 JSON（空切片也输出 [] 而非 null）。
func (in *ReviewInput) TimelineJSON() string {
	if in == nil || len(in.Timeline) == 0 {
		return "[]"
	}
	b, err := json.Marshal(in.Timeline)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// BrainReviewInput is an immutable session-owned evidence projection.
type BrainReviewInput struct {
	Faults        []json.RawMessage     `json:"faults"`
	Evidence      []BrainReviewEvidence `json:"evidence"`
	Challenges    []json.RawMessage     `json:"challenges"`
	Assessment    json.RawMessage       `json:"assessment,omitempty"`
	Sources       []string              `json:"sources"`
	CleanupStatus string                `json:"cleanup_status"`
}
type BrainReviewEvidence struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Actor   string          `json:"actor"`
	Payload json.RawMessage `json:"payload"`
}
type BrainReviewClaim struct {
	Section      string   `json:"section,omitempty"`
	Category     string   `json:"category"`
	Text         string   `json:"text"`
	EvidenceRefs []string `json:"evidence_refs"`
	SourceURLs   []string `json:"source_urls"`
}
