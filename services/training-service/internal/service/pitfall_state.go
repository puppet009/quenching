package service

// State 一次训练会话内坑点引擎的运行时状态（编排层按会话持有）。
//
// MVP 只需要"每个坑点已注入几次"来执行 max_times 上限；后续 T3-11 会在此扩展
// 每个坑点的"已识别/已踩坑"标记。State 与具体存储无关：训练进行中由编排层在
// 内存维护并可从 ai_messages 重建，避免给纯逻辑引擎引入数据库依赖。
type State struct {
	injected map[int64]int
}

// NewState 创建空状态。
func NewState() State {
	return State{injected: map[int64]int{}}
}

// RecordInjection 记录某坑点成功注入一次（在助手坑点消息落库成功后调用）。
// 指针接收者：零值 State 的 injected 为 nil 时需就地初始化并回写，
// 值接收者会让新建的 map 只存在于副本上，导致计数静默丢失。
func (s *State) RecordInjection(configID int64) {
	if s.injected == nil {
		s.injected = map[int64]int{}
	}
	s.injected[configID]++
}

// InjectedCount 返回某坑点已注入次数。
func (s State) InjectedCount(configID int64) int {
	if s.injected == nil {
		return 0
	}
	return s.injected[configID]
}

// TotalInjected 返回本会话已注入坑点总数（供 training_sessions.pitfall_total 统计）。
func (s State) TotalInjected() int {
	n := 0
	for _, c := range s.injected {
		n += c
	}
	return n
}
