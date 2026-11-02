// 方法论复盘模板配置（T3-2：复盘模板按故障类型配置化）。
//
// 方法级复盘（method-v3，T3-3 校准）的 system instruction 除了通用的六章输出约束外，
// 还要按本次故障类型注入三类"关注点素材"，引导 LLM 产出贴故障域的内容：
//   - Methodology：该类故障可迁移的通用排查框架；
//   - Knowledge：  知识点梳理应覆盖的核心概念与工具；
//   - Highlights： 亮点坑点评判时应关注的操作习惯与典型误区。
//
// 当前实现是静态内置注册表（StaticProvider），覆盖 MVP 首批 8 种基础故障，
// 并对查不到的故障类型回退 generic 通用模板（不报错——故障分类词表尚未受控）。
// T3-3 负责各模板要点内容的校准；未来后台管理入库后，只需替换 Provider 实现，
// 调用方（service 层生成器）与端口签名均不用改。
package service

import "strings"

// Template 单个故障类型的方法论复盘模板。
//
// Key 为规范标识（小写英文 token，如 slow_sql）；DisplayName 为中文名。
// 三个要点字段为注入 system instruction 的素材，均为简短的中文条目（分号分隔），
// 不是成稿 Prompt，由 Prompt 构建层统一拼装。
type Template struct {
	// Key 规范故障类型标识；generic 模板为 GenericKey。
	Key string
	// DisplayName 故障类型中文名（如"慢SQL"）。
	DisplayName string
	// Methodology 方法论排查框架要点。
	Methodology string
	// Knowledge 知识点梳理关注点。
	Knowledge string
	// Highlights 亮点坑点评判关注点。
	Highlights string
}

// GenericKey 通用兜底模板的规范标识。
const GenericKey = "generic"

// Provider 复盘模板供应端口。
//
// 按故障类型返回模板：命中具体故障返回其专属模板；未命中（含空串/未知类型）
// 必须返回 generic 兜底模板而不是错误，保证复盘生成不因分类缺失而中断。
type Provider interface {
	TemplateByFaultType(faultType string) Template
}

// IsGeneric 报告该模板是否为通用兜底模板。
func (t Template) IsGeneric() bool {
	return t.Key == GenericKey
}

// PromptHints 把模板要点渲染成可注入 system instruction 的中文片段。
//
// 固定三段标题与 Prompt 的六章指令呼应；generic 模板同样可用，
// 因此调用方无需区分是否命中具体故障类型。
func (t Template) PromptHints() string {
	var b strings.Builder
	b.WriteString("【按故障类型的复盘关注点】\n")
	b.WriteString("方法论提炼参考框架：")
	b.WriteString(t.Methodology)
	b.WriteString("\n知识点梳理关注点：")
	b.WriteString(t.Knowledge)
	b.WriteString("\n亮点坑点评判关注点：")
	b.WriteString(t.Highlights)
	return b.String()
}
