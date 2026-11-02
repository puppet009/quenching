// 操作代价计分相关共享类型（T2-17）。
//
// 这些类型同时被 repository（操作时间线映射）与 service（代价计分规则）
// 引用，故置于 model；代价计分的规则与配置本身在 service 包。
package model

// OperationType 操作类型。
type OperationType string

const (
	OpViewMetric     OperationType = "view_metric"     // 查看指标
	OpViewLog        OperationType = "view_log"        // 查看日志
	OpViewConfig     OperationType = "view_config"     // 查看配置
	OpViewAlert      OperationType = "view_alert"      // 查看告警
	OpExecCommand    OperationType = "exec_command"    // 执行命令
	OpModifyConfig   OperationType = "modify_config"   // 修改配置
	OpScaleResource  OperationType = "scale_resource"  // 扩容资源
	OpRestartService OperationType = "restart_service" // 重启服务
	OpAIQuestion     OperationType = "ai_question"     // AI 提问
	OpFixComplete    OperationType = "fix_complete"    // 完成修复
	OpOther          OperationType = "other"           // 其他
)

// OperationAction 单条操作记录。
type OperationAction struct {
	// Type 操作类型。
	Type OperationType
	// TimestampMs 操作发生时间（相对训练开始，毫秒）。
	TimestampMs int64
	// IsEffective 是否为有效操作（推动了排障进展）。
	// MVP 阶段可简化：信息收集默认有效，变更操作需根据结果判断。
	IsEffective bool
}
