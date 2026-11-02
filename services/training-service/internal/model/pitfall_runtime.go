// AI 坑点（规则版）的跨端口运行时类型：判定时刻的上下文、用户操作、反应类型。
//
// 边界：这些类型被 service 判定层与 repository（写 ai_messages.user_response_type）
// 共同使用，落 model 以避免 repository 反向依赖 service。坑点结局的强类型
// service.PitfallOutcome 仍留在 service（事件层口径，见 collab_events.go）。
package model

// 用户反应类型（写 ai_messages.user_response_type，VARCHAR(20)）。
const (
	ResponseAgree    = "agree"    // 采纳 AI 建议（踩坑）
	ResponseDispute  = "dispute"  // 反驳/质疑 AI
	ResponseVerified = "verified" // 先独立验证后走对
	ResponseIgnore   = "ignore"   // 忽略建议、沿其他方向排查
)

// PitfallOperation 坑点注入后用户在沙箱/终端里发生的一条操作（映射 session_operations）。
// 只保留判定所需字段，与具体存储无关；Type 取 operation_type（view_metric/
// view_log/exec_command/restart_service/modify_config/...），Category 取
// operation_category（observe/analyze/action/ai_assist），均允许配置按任一口径匹配。
type PitfallOperation struct {
	Type     string // view_metric/view_log/exec_command/restart_service/modify_config/...
	Category string // observe/analyze/action/ai_assist
	Target   string
}

// PitfallRuntimeContext 一次"判定时刻"编排层能提供的运行时上下文（与数据库无关）。
//
// 坑点引擎只依据这些输入做纯函数式判定，便于在无 PG/无 Python 环境下单测：
//   - Turn        当前用户这是第几轮发言（从 1 起）。
//   - Question    本轮用户提问原文（question_match 用）。
//   - Stage       当前排障阶段标识（stage_enter 用，与编排层口径对齐）。
//   - ElapsedSec  距实训开始已过去的秒数（delay/window 用）。
//   - TriggeredDefects 本会话内已暴露（triggered 及之后状态）的缺陷集合，
//     按缺陷精确判定前置 requires.fault_triggered（一次会话可挂多个缺陷）。
type PitfallRuntimeContext struct {
	Turn             int
	Question         string
	Stage            string
	ElapsedSec       int
	TriggeredDefects map[int64]bool
}
