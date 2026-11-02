// prompt.go 基础复盘生成（T2-29），随 internal/generator 野包归位并入 service 层（g4）。
//
// 职责边界：
//   - 本文件只做"取数 → 组装 Prompt 输入 → 调 ai-service → 解析结构化章节"的编排；
//   - 不碰 RabbitMQ（review_worker.go 负责）、不碰 reviews/review_sections 表写回
//     （worker 经 ReviewTaskRepo.ReplaceSections 负责，章节映射规则在 model.BasicSections）；
//   - 对 AI 输出做 JSON schema 强校验（两章必填、必须是字符串、非空白），
//     不合格直接上抛错误，由 worker 的重试机制（最多 3 次）兜底。
//
// 可测试性：Prompt 构建（BuildPrompt）与章节解析（ParseSections）均为纯函数；
// 输入数据与 AI 调用是两个端口（InputProvider / ReviewAIClient），单测注入内存 fake。
package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quenching/review-service/internal/model"
)

// PromptVersion 基础复盘 Prompt 版本号。
// Prompt 模板调整时递增，随 reviews.prompt_version 持久化以便回溯/A-B。
const PromptVersion = "basic-v1"

// MethodPromptVersion 方法论复盘 Prompt 版本号。
// method-v1（T3-1）：三章（根因/时间线/路径对比）；
// method-v2（T3-2）：六章（再加亮点坑点/方法论/知识点），并按故障类型注入模板要点；
// method-v3（T3-3）：8 故障模板要点逐域校准，指令强化"要点必须与时间线证据
// 绑定、不得罗列与本次故障无关的概念/命令"，提升抽检复盘质量。
const MethodPromptVersion = "method-v3"

// systemInstruction 是基础复盘的角色与输出约束（T2-29）。
// 要求 LLM 只基于给定事实、输出严格 JSON、两章为必填；
// ai-service 六段式中的其余章节基础复盘不消费，故不要求。
const systemInstruction = `你是故障演练复盘报告生成器，负责"基础复盘"。
你将收到一次故障演练的操作时间线、故障剧本与指标快照。
请严格基于输入中的事实分析，不得臆造时间线中不存在的操作或指标。
输出必须是且只能是一个 JSON 对象，使用如下结构：
{
  "root_cause": "根因分析：结合故障剧本与指标拐点，说明故障传播路径与真正根因，指出排障过程中的关键判断节点，200-400字",
  "timeline": "时间线解读：按时间顺序重述关键操作并标注拐点（故障注入、有效观察、无效尝试、定位、修复），200-400字"
}
只输出 JSON，不要输出 Markdown 代码块或任何解释。两章均不得为空。`

// methodSystemInstructionTemplate 是方法论复盘的角色与输出约束模板（method-v3，T3-3 校准）。
//
// 含一个 %s 占位，运行时由按故障类型选中的复盘模板要点填充
// （Template.PromptHints）：六章中的亮点坑点/方法论/知识点
// 要结合该故障域的排查框架产出；查不到专属模板时注入 generic 通用要点。
//
// 输出约束：在 T3-1 三章（根因/时间线/路径对比）之外，T3-2 强制要求
// highlights_pitfalls（基于时间线的亮点与典型错误概述，非 AI 逐坑点明细）、
// methodology（可迁移排查框架）、knowledge（核心概念与延伸阅读）三章，共六章。
// 所有结论必须基于时间线/剧本中真实存在的操作与耗时，不得臆造。
const methodSystemInstructionTemplate = `你是故障演练复盘报告生成器，负责"方法论复盘"。
你将收到一次故障演练的操作时间线、故障剧本（含识别/修复耗时）与指标快照。
请严格基于输入中的事实分析，不得臆造时间线中不存在的操作、指标或耗时。
%s
输出必须是且只能是一个 JSON 对象，使用如下结构：
{
  "root_cause": "根因分析：结合故障剧本与指标拐点，说明故障传播路径与真正根因，指出排障过程中的关键判断节点，200-400字",
  "timeline": "时间线解读：按时间顺序重述关键操作并标注拐点（故障注入、有效观察、无效尝试、定位、修复），200-400字",
  "path_comparison": "路径对比：先概述用户的实际排查路径，再对照该故障类型的专家最优路径，逐段指出哪里走了弯路（无效观察、重复尝试、错误方向、定位后修复动作绕路），结合剧本的识别/修复耗时估算每处弯路浪费的时间，最后给出更优的动作序列，300-500字",
  "highlights_pitfalls": "亮点与坑点：基于操作时间线概述，先讲本次做得好的地方（有效观察、正确判断、规范动作），再讲典型错误操作（无效尝试、错误方向、危险动作），并说明这些操作为什么对/为什么错，200-400字",
  "methodology": "方法论提炼：结合上述方法论参考框架，提炼该类故障可迁移的通用排查方法（观察现象→定位手段→根因判断→修复验证的套路），不要只复述本次经过，200-400字",
  "knowledge": "知识点梳理：围绕上述知识点关注点，解释本次涉及的核心技术概念、命令/工具的用途与判读方法，并给出延伸阅读方向，200-400字"
}
只输出 JSON，不要输出 Markdown 代码块或任何解释。六章均不得为空；
path_comparison 必须落到具体操作与时间，不能只给空泛建议；
highlights_pitfalls 只评价用户自己的操作，不要罗列 AI 助手的逐点表现；
methodology 与 knowledge 必须以本次故障现象和时间线中的真实操作为锚点，
结合注入的关注点素材展开，只讲本次实际用到或明显相关的概念、命令与判读方法，
不要罗列与本次故障无关的知识清单，也不要给出时间线中没有依据的操作或数值。`

// promptEnvelope 是 operation_timeline_json 的载荷结构。
// proto 的时间线字段本只承载操作数组，这里用信封把剧本/指标/输出要求一并
// 透传给 ai-service（它原样拼进 user prompt），无需改动跨语言 proto 契约。
//
// Kind 复用 "basic_review_input"：Python 端只凭该 kind 识别信封并把
// instruction 原样作为 system prompt，因此方法论复盘无需改 Python 契约；
// Level 为可选观测字段（basic 省略、method 标注），Python 端忽略未知键。
type promptEnvelope struct {
	Brain       *model.BrainReviewInput   `json:"brain,omitempty"`
	Kind        string                    `json:"kind"` // 固定 "basic_review_input"
	Level       string                    `json:"level,omitempty"`
	Instruction string                    `json:"instruction"`
	SessionID   int64                     `json:"session_id"`
	FaultType   string                    `json:"fault_type,omitempty"`
	Difficulty  string                    `json:"difficulty,omitempty"`
	Timeline    []model.TimelineOperation `json:"timeline"`
	Scripts     []model.FaultScript       `json:"scripts"`
	Metrics     []model.MetricSnapshot    `json:"metrics,omitempty"`
}

// Prompt 组装好的 AI 输入。
type Prompt struct {
	// FaultType 故障类型，对应 proto GenerateReviewRequest.fault_type。
	FaultType string
	// TimelineJSON 操作时间线 JSON（基础复盘用信封承载完整输入）。
	TimelineJSON string
	// PitfallJSON 坑点记录 JSON。基础复盘不含坑点分析，固定空数组。
	PitfallJSON string
}

// BuildPrompt 把复盘输入组装成基础复盘 AI 调用所需的 Prompt（纯函数）。
// 任何缺失数据都降级为空集合，保证可生成（缺数据时由 LLM 据已有信息作答）。
func BuildPrompt(in *model.ReviewInput) Prompt {
	return buildEnvelopePrompt(in, "", systemInstruction)
}

// BuildMethodPrompt 把复盘输入组装成方法论复盘 AI 调用所需的 Prompt（method-v3，T3-3 校准）。
// 与基础版共用信封结构；指令扩为六章，并把按故障类型选中的模板要点注入 system
// instruction（tpl 由 Provider 选出，generic 兜底亦可）。
func BuildMethodPrompt(in *model.ReviewInput, tpl Template) Prompt {
	instruction := fmt.Sprintf(methodSystemInstructionTemplate, tpl.PromptHints())
	return buildEnvelopePrompt(in, model.ReviewLevelMethod, instruction)
}

// buildEnvelopePrompt 组装信封；level 为空表示基础复盘（信封省略 level 键）。
func buildEnvelopePrompt(in *model.ReviewInput, level, instruction string) Prompt {
	env := promptEnvelope{
		Kind:        "basic_review_input",
		Level:       level,
		Instruction: instruction,
		Timeline:    []model.TimelineOperation{},
		Scripts:     []model.FaultScript{},
	}
	faultType := ""
	if in != nil {
		env.Brain = in.Brain
		env.SessionID = in.SessionID
		env.FaultType = in.FaultType
		env.Difficulty = in.Difficulty
		faultType = in.FaultType
		if len(in.Timeline) > 0 {
			env.Timeline = in.Timeline
		}
		if len(in.Scripts) > 0 {
			env.Scripts = in.Scripts
		}
		if len(in.Metrics) > 0 {
			env.Metrics = in.Metrics
		}
	}

	b, err := json.Marshal(env)
	if err != nil {
		// 输入均为可控的基础类型，理论上不会失败；失败时退回最小信封。
		b = []byte(`{"kind":"basic_review_input","timeline":[],"scripts":[]}`)
	}

	return Prompt{
		FaultType:    faultType,
		TimelineJSON: string(b),
		PitfallJSON:  "[]",
	}
}

// ParseSections 解析 ai-service 返回的 sections_json（纯函数）。
// 兼容两种返回：
//   - 基础两段式：{"root_cause": "...", "timeline": "..."}
//   - ai-service 现行六段式：root_cause/timeline/path_comparison/...
//
// 容忍 LLM 常见的 Markdown 代码块包裹。两章任一为空/非字符串即返回错误，
// 由 worker 的重试机制（最多3次）兜底。
func ParseSections(sectionsJSON string) (*model.ReviewSections, error) {
	m, err := decodeSections(sectionsJSON)
	if err != nil {
		return nil, err
	}

	rootCause, err := readStringField(m, model.SectionRootCause)
	if err != nil {
		return nil, err
	}
	timeline, err := readStringField(m, model.SectionTimeline)
	if err != nil {
		return nil, err
	}

	sections := &model.ReviewSections{
		RootCause: strings.TrimSpace(rootCause),
		Timeline:  strings.TrimSpace(timeline),
	}
	if !sections.Valid() {
		return nil, ErrIncompleteSections
	}
	return sections, nil
}

// ParseMethodSections 解析方法论复盘六章（method-v3，纯函数）。
//
// 必填六章：root_cause/timeline/path_comparison/highlights_pitfalls/
// methodology/knowledge，必须齐全且为非空字符串。
// 对 ai-service 六段式中的其余章节（pitfall_analysis/collab_review）容忍忽略：
// pitfall_analysis 是 AI 挖坑/踩坑逐点明细（数组/字符串混合类型），语义归 T3-13，
// 本函数只按字符串读取本卡消费的六个键。
// 容忍 Markdown 代码块包裹；不合格即返回错误，由 worker 重试兜底。
func ParseMethodSections(sectionsJSON string) (*model.ReviewSections, error) {
	m, err := decodeSections(sectionsJSON)
	if err != nil {
		return nil, err
	}

	rootCause, err := readStringField(m, model.SectionRootCause)
	if err != nil {
		return nil, err
	}
	timeline, err := readStringField(m, model.SectionTimeline)
	if err != nil {
		return nil, err
	}
	pathComparison, err := readStringField(m, model.SectionPathComparison)
	if err != nil {
		return nil, err
	}
	highlightsPitfalls, err := readStringField(m, model.SectionHighlightsPitfalls)
	if err != nil {
		return nil, err
	}
	methodology, err := readStringField(m, model.SectionMethodology)
	if err != nil {
		return nil, err
	}
	knowledge, err := readStringField(m, model.SectionKnowledge)
	if err != nil {
		return nil, err
	}

	sections := &model.ReviewSections{
		RootCause:          strings.TrimSpace(rootCause),
		Timeline:           strings.TrimSpace(timeline),
		PathComparison:     strings.TrimSpace(pathComparison),
		HighlightsPitfalls: strings.TrimSpace(highlightsPitfalls),
		Methodology:        strings.TrimSpace(methodology),
		Knowledge:          strings.TrimSpace(knowledge),
	}
	if !sections.MethodFullValid() {
		return nil, ErrIncompleteMethodSections
	}
	return sections, nil
}

// decodeSections 剥离代码块、判空、反序列化为字段 map（基础/方法论解析共用）。
func decodeSections(sectionsJSON string) (map[string]json.RawMessage, error) {
	raw := strings.TrimSpace(stripCodeFence(sectionsJSON))
	if raw == "" {
		return nil, ErrEmptySections
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, &ParseError{Cause: err}
	}
	return m, nil
}

// stripCodeFence 去掉 LLM 常输出的 ```json ... ``` 包裹。
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	// 去掉可能的 json 语言标记
	s = strings.TrimPrefix(s, "json")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return s
}

// readStringField 从 map 中取字符串字段；缺失或类型不符返回错误。
func readStringField(m map[string]json.RawMessage, key string) (string, error) {
	raw, ok := m[key]
	if !ok {
		return "", &MissingFieldError{Field: key}
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", &FieldTypeError{Field: key, Cause: err}
	}
	return v, nil
}
