// 主动线索推送模型（T3-7）。
//
// 消费 T3-6 卡顿扫描器的 OnStall 钩子：会话由正常翻转为卡顿时，向学员主动推送
// 一条提问式引导线索（走 ws-gateway 的 ai_message 事件），并把线索作为助手消息
// 落库 ai_messages，保证事件错过/未在线时历史仍可回看，同时以落库事实做跨重启、
// 多副本安全的频控。
//
// 设计边界（对应任务验收"卡顿时主动推送，正常不打扰"）：
//   - 只在 normal → stalled 翻转时推送，正常状态零打扰；
//   - 同一会话同一卡顿周期内、同一卡顿原因（idle/no_progress）只推一条；
//   - 线索文案为确定性提问式模板，不经 LLM、不读任何答案列（root_cause 等），
//     不含阈值与证据数值，绝不泄露故障根因；
//   - 推送依赖（PG / ws-gateway）任一不可用均静默降级，只记日志，不影响卡顿扫描。
package model

import "time"

// 默认超时配置（Normalize 时补零值）。
const (
	// defaultPersistTimeout 查属主/频控/落库的整体超时。
	defaultPersistTimeout = 10 * time.Second
	// defaultPublishTimeout 发布 ai_message 事件的超时。
	defaultPublishTimeout = 5 * time.Second
)

// ClueConfig 主动线索推送配置。
type ClueConfig struct {
	// Enabled 总开关；false 时不注册任何推送动作。
	Enabled bool
	// PersistTimeout 查属主/频控判定/线索落库的整体超时。
	PersistTimeout time.Duration
	// PublishTimeout 发布事件到 ws-gateway 的超时。
	PublishTimeout time.Duration
}

// Normalize 补默认值并做下限保护，返回一份归一配置。
func (c ClueConfig) Normalize() ClueConfig {
	if c.PersistTimeout <= 0 {
		c.PersistTimeout = defaultPersistTimeout
	}
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = defaultPublishTimeout
	}
	return c
}
