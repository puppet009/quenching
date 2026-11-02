// 线索文案（T3-7）。
//
// 内容安全红线：线索只做"提问式引导"，帮助学员自己描述现象、缩小排查方向，
// 绝不给出结论或答案。因此：
//   - 不经 LLM 生成（避免模型结合上下文吐出根因）；
//   - 不读取 defects.root_cause、修复配置等任何答案列；
//   - 文案不含判定阈值、证据数值与"因为/故障是"等结论性措辞。
package service

import "github.com/quenching/training-service/internal/model"

// clueTemplates 按卡顿原因给出确定性线索文案。
var clueTemplates = map[string]string{
	string(model.StallReasonIdle): "看起来你在这一步停留了一会儿，不着急。可以先和我说说你目前观察到的现象：" +
		"错误率、响应延迟、流量这几项里，哪一项最让你觉得不对劲？",
	string(model.StallReasonNoProgress): "你已经做了几次尝试，但指标似乎还没有明显起色。" +
		"要不要先把范围缩小一点：你觉得问题更可能出现在请求入口、下游依赖，还是服务自身的资源上？" +
		"选一个方向，我们一起验证。",
}

// fallbackTemplate 未知原因时的通用引导（理论上不会出现，防御性兜底）。
const fallbackTemplate = "如果暂时没有头绪，可以先和我描述一下现在的现象，我们一起慢慢梳理排查方向。"

// RenderClue 按卡顿原因返回线索文案；未知原因返回通用兜底话术。
func RenderClue(reason string) string {
	if text, ok := clueTemplates[reason]; ok {
		return text
	}
	return fallbackTemplate
}
