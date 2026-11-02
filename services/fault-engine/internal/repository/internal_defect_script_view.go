// 服务间内部查询专用的"剧本详情"投影与只读端口。
//
// 与 internal_defect_view.go 的脱敏通道严格分离：该投影包含答案列 root_cause，
// 仅供 training-service 的复盘聚合端点在服务端组装 AI 复盘输入时调用，
// 不挂网关、不对前端暴露，也不得用于任何用户面响应。
// 其余注入配置（trigger_config、manifestation_config、fix_config、
// amplifier_config、freeze_rules）仍严禁流出。
package repository

import "context"

// InternalDefectScriptView 故障剧本详情投影（含预设根因，仅复盘 AI 内部使用）。
type InternalDefectScriptView struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Difficulty  string `json:"difficulty"`
	Description string `json:"description"`
	// RootCause 剧本预设根因（答案列）：供复盘 AI 与学员实际排障结论做对照。
	RootCause string `json:"root_cause"`
}

// InternalDefectScriptReader 剧本详情服务间只读查询端口（含答案列，调用方受限）。
type InternalDefectScriptReader interface {
	// ListScriptsByIDs 按 ID 列表批量返回剧本详情；
	// 缺失或已软删的 ID 不占结果位，调用方按返回的 ID 自行对齐。
	ListScriptsByIDs(ctx context.Context, ids []int64) ([]*InternalDefectScriptView, error)
}
