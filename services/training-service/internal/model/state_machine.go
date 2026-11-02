package model

// allowedTransitions 训练会话合法状态流转表。
// 非法跳转必须拒绝，防止跨服务并发/重试导致状态错乱。
var allowedTransitions = map[SessionStatus]map[SessionStatus]bool{
	StatusPending: {
		StatusDeploying: true,
		StatusAborted:   true, // 创建后放弃
		StatusFailed:    true,
	},
	StatusDeploying: {
		StatusArmed:   true, // 沙箱就绪、故障武装完成
		StatusRunning: true, // 无故障演练等场景可直接进入
		StatusAborted: true, // 部署失败/用户取消
		StatusFailed:  true,
	},
	StatusArmed: {
		StatusRunning: true, // 实训正式开始（故障可能延迟触发）
		StatusAborted: true,
		StatusFailed:  true,
	},
	StatusRunning: {
		StatusResolved:  true, // 故障被判定修复
		StatusCompleted: true, // 无故障或直接结束
		StatusAborted:   true, // 用户主动结束/超时
		StatusFailed:    true,
	},
	StatusResolved: {
		StatusCompleted: true, // 修复后进入复盘
		StatusAborted:   true,
		StatusFailed:    true,
	},
	// 终态
	StatusCompleted: {},
	StatusAborted:   {},
	StatusFailed:    {},
}

// CanTransition 判断状态能否从 from 流转到 to。
func CanTransition(from, to SessionStatus) bool {
	if from == to {
		return true // 幂等：重复事件到达同态合法
	}
	return allowedTransitions[from][to]
}

// IsTerminal 是否终态。
func IsTerminal(s SessionStatus) bool {
	switch s {
	case StatusCompleted, StatusAborted, StatusFailed:
		return true
	default:
		return false
	}
}

// IsActive 是否仍在进行（占用沙箱、可能有故障注入）。
func IsActive(s SessionStatus) bool {
	switch s {
	case StatusDeploying, StatusArmed, StatusRunning, StatusResolved:
		return true
	default:
		return false
	}
}
