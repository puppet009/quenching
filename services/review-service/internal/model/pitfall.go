// AI 坑点分析数据模型（T3-13 坑点复盘章节）。
//
// 坑点复盘章节的数据来源：
//   - ai_messages.is_pitfall = true 的 AI 消息（本次会话中出现过哪些坑点）
//   - ai_messages.user_response_type（用户踩了还是识别了）
//   - ai_pitfall_configs（坑点配置详情：错误内容、正确做法、严重程度等）
//
// 输出形态：
//   - review_sections 表新增 fix_ai_pitfall_analysis 章节（Markdown 正文）
//   - reviews.ai_pitfall_count / ai_pitfall_recognized 统计更新
//   - content_data 存结构化坑点列表（供前端卡片渲染，T3-29 D4 对接）

package model

// 用户对坑点的响应类型（与 ai_messages.user_response_type 对齐）。
const (
	PitfallResponseAgreed   = "agreed"   // 踩坑：采纳了错误建议
	PitfallResponseDisputed = "disputed" // 识别：反驳/质疑了错误建议
	PitfallResponseVerified = "verified" // 识别：验证后走对了
	PitfallResponseIgnored  = "ignored"  // 识别：忽略了错误建议
	PitfallResponsePending  = "pending"  // 未响应（用户还没操作）
)

// PitfallType 坑点类型。
const (
	PitfallTypeCausalMisjudgment = "causal_misjudgment" // 因果误判
	PitfallTypeInfoOmission      = "info_omission"      // 信息遗漏
	PitfallTypeOverconfidence    = "overconfidence"     // 过度自信
)

// PitfallItem 单个坑点的复盘分析项。
// 对应 content_data 数组中的一项，前端按卡片渲染。
type PitfallItem struct {
	// PitfallConfigID 坑点配置 ID。
	PitfallConfigID int64 `json:"pitfall_config_id"`
	// PitfallType 坑点类型：causal_misjudgment / info_omission / overconfidence。
	PitfallType string `json:"pitfall_type"`
	// Name 坑点名称。
	Name string `json:"name"`
	// Description 坑点描述（为什么这是个坑）。
	Description string `json:"description"`
	// Severity 严重程度：low / medium / high。
	Severity string `json:"severity"`
	// ErrorContent AI 错误建议内容（即坑点注入时说了什么）。
	ErrorContent string `json:"error_content"`
	// CorrectApproach 正确做法。
	CorrectApproach string `json:"correct_approach"`
	// UserResponse 用户响应类型：agreed / disputed / verified / ignored。
	UserResponse string `json:"user_response"`
	// Recognized 用户是否识别了坑点（agreed = false，其余 = true）。
	Recognized bool `json:"recognized"`
	// MessageID 对应的 AI 消息 ID。
	MessageID int64 `json:"message_id"`
	// AppearOffsetSec 坑点出现的相对时间（秒），用于时间线定位。
	AppearOffsetSec int `json:"appear_offset_sec"`
}

// PitfallAnalysis 坑点分析汇总（章节结构化数据）。
type PitfallAnalysis struct {
	// TotalCount 坑点总数。
	TotalCount int `json:"total_count"`
	// RecognizedCount 识别数。
	RecognizedCount int `json:"recognized_count"`
	// RecognitionRate 识别率（0-100）。
	RecognitionRate float64 `json:"recognition_rate"`
	// Pitfalls 坑点明细列表，按出现时间升序。
	Pitfalls []PitfallItem `json:"pitfalls"`
}

// Recognized 判断用户是否识别了坑点。
func (p *PitfallItem) IsRecognized() bool {
	return p.UserResponse != PitfallResponseAgreed &&
		p.UserResponse != PitfallResponsePending &&
		p.UserResponse != ""
}

// PitfallTypeLabel 坑点类型中文标签。
func PitfallTypeLabel(t string) string {
	switch t {
	case PitfallTypeCausalMisjudgment:
		return "因果误判"
	case PitfallTypeInfoOmission:
		return "信息遗漏"
	case PitfallTypeOverconfidence:
		return "过度自信"
	default:
		return t
	}
}

// SeverityLabel 严重程度中文标签。
func SeverityLabel(s string) string {
	switch s {
	case "low":
		return "低"
	case "medium":
		return "中"
	case "high":
		return "高"
	default:
		return s
	}
}

// ResponseLabel 用户响应中文标签。
func ResponseLabel(r string) string {
	switch r {
	case PitfallResponseAgreed:
		return "踩坑（采纳了错误建议）"
	case PitfallResponseDisputed:
		return "识别（提出质疑）"
	case PitfallResponseVerified:
		return "识别（验证后纠正）"
	case PitfallResponseIgnored:
		return "识别（忽略了建议）"
	case PitfallResponsePending:
		return "未响应"
	default:
		return r
	}
}

// pitfallTitle 坑点章节标题。
const PitfallSectionTitle = "AI 坑点分析"

// pitfallSectionOrder 坑点章节排序（在基础两章之后）。
const PitfallSectionOrder = 30

// pitfallSectionLevel 坑点章节级别（方法论版启用，但基础版也展示）。
const PitfallSectionLevel = SectionLevelMethod
