package model

// allowedTransitions 合法状态流转表。非法流转必须拒绝，防止状态错乱。
// 调谐器(reconcile)依赖这张表保证幂等：只从当前合法状态往下推进。
var allowedTransitions = map[string]map[string]bool{
	StatusCreating:  {StatusDeploying: true, StatusFailed: true},
	StatusDeploying: {StatusReady: true, StatusFailed: true},
	StatusReady:     {StatusRunning: true, StatusReleasing: true, StatusFailed: true},
	StatusRunning:   {StatusReleasing: true, StatusFailed: true},
	StatusReleasing: {StatusReleased: true, StatusFailed: true},
	StatusFailed:    {StatusReleasing: true}, // 失败也要回收资源
	StatusReleased:  {},                      // 终态
}

// CanTransition 判断从 from 到 to 是否合法。
func CanTransition(from, to string) bool {
	if from == to {
		return true // 幂等：调谐时重复到达同态视为合法
	}
	return allowedTransitions[from][to]
}

// IsTerminal 是否终态（终态后无需再调谐）。
func IsTerminal(status string) bool {
	return status == StatusReleased
}

// IsActive 沙箱是否仍占用集群资源（未释放）。
func IsActive(status string) bool {
	switch status {
	case StatusReleased, StatusFailed:
		// failed 仍可能残留资源，需要回收；这里返回 true 以便调谐器清理。
		return status == StatusFailed
	default:
		return true
	}
}
