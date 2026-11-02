// AI 坑点（规则版）的配置模型：ai_pitfall_configs 一行的可执行部分。
//
// 边界（T3-8）：训练启动时按缺陷加载关联的 ai_pitfall_configs，把 JSONB
//   - trigger_condition（触发条件：时机+概率+前置状态）
//   - recognition_rules（何为"识别了坑"）
//   - pitfall_rules（何为"踩了坑"）
//
// 解析为强类型并校验。本文件只负责"配置可被正确表达与校验"；
// 真正的触发判定、错误建议注入、踩坑/识别状态推进在 service 层（T3-9/11）。
//
// 设计依据：《技术架构文档》§11.6（规则版坑点：时机/阶段/概率触发）、
// 《数据库设计文档》§11.4 ai_pitfall_configs。
package model

import "time"

// 坑点类型（ai_pitfall_configs.pitfall_type）。
const (
	TypeCausalMisjudgment = "causal_misjudgment" // 因果误判：把相关性当因果性
	TypeInfoOmission      = "info_omission"      // 信息遗漏：漏掉关键线索
	TypeOverconfidence    = "overconfidence"     // 过度自信：未经验证的高置信结论
)

// 配置状态。
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// 置信度表现；Random 表示在一次训练内随机分布，避免用户学到"越自信越是坑"的规律。
const (
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
	ConfidenceHigh   = "high"
	ConfidenceRandom = "random"
)

// 危险程度（踩坑后故障恶化程度）。
const (
	SeverityLow    = "low"
	SeverityMedium = "medium"
	SeverityHigh   = "high"
)

var (
	supportedTypes      = map[string]bool{TypeCausalMisjudgment: true, TypeInfoOmission: true, TypeOverconfidence: true}
	supportedStatus     = map[string]bool{StatusDraft: true, StatusActive: true, StatusInactive: true}
	supportedConfidence = map[string]bool{ConfidenceLow: true, ConfidenceMedium: true, ConfidenceHigh: true, ConfidenceRandom: true}
	supportedSeverity   = map[string]bool{SeverityLow: true, SeverityMedium: true, SeverityHigh: true}
)

// PitfallConfig 一条坑点配置（ai_pitfall_configs 一行的可执行部分）。
type PitfallConfig struct {
	ID              int64            `json:"id"`
	DefectID        int64            `json:"defect_id"`
	PitfallType     string           `json:"pitfall_type"`
	Name            string           `json:"name"`
	Description     string           `json:"description,omitempty"`
	Trigger         TriggerCondition `json:"trigger_condition"`
	ErrorContent    string           `json:"error_content"`     // AI 错误建议的 Prompt 片段
	ConfidenceLevel string           `json:"confidence_level"`  // low/medium/high/random
	Recognition     RecognitionRules `json:"recognition_rules"` // T3-11 消费
	PitfallRules    FallRules        `json:"pitfall_rules"`     // T3-11 消费
	CorrectApproach string           `json:"correct_approach,omitempty"`
	Severity        string           `json:"severity"`
	Status          string           `json:"status"`
	SortOrder       int              `json:"sort_order"`
	CreatedBy       int64            `json:"created_by,omitempty"`
	CreatedAt       time.Time        `json:"created_at,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at,omitempty"`
}

// TriggerWhen 触发时机类型（技术架构 §11.6：时机/阶段/概率，外加最稳定的延迟触发）。
type TriggerWhen string

const (
	// WhenAlways 只要满足前置状态即可触发（再走概率），最简单稳定，确定性模板主用。
	WhenAlways TriggerWhen = "always"
	// WhenQuestionMatch 用户提问命中关键词/模式时触发（"时机触发"）。
	WhenQuestionMatch TriggerWhen = "question_match"
	// WhenStageEnter 排障进入指定阶段时触发（"阶段触发"）。
	WhenStageEnter TriggerWhen = "stage_enter"
	// WhenDelay 实训开始后达到延迟秒数（或落在时间窗内）时触发。
	WhenDelay TriggerWhen = "delay"
)

var supportedWhen = map[TriggerWhen]bool{
	WhenAlways: true, WhenQuestionMatch: true, WhenStageEnter: true, WhenDelay: true,
}

// TimeWindow 相对实训开始的秒级窗口 [StartSeconds, EndSeconds)。
type TimeWindow struct {
	StartSeconds int `json:"start_seconds"`
	EndSeconds   int `json:"end_seconds"`
}

// TriggerRequires 触发前置状态：全部满足才允许进入"时机/概率"判定。
type TriggerRequires struct {
	// FaultTriggered 故障是否必须已暴露为故障（默认要求，避免故障还没发生就给错误建议）。
	FaultTriggered *bool `json:"fault_triggered,omitempty"`
	// MinTurn / MaxTurn 限定在第几到第几轮对话窗口内（0 表示不限）。
	MinTurn int `json:"min_turn,omitempty"`
	MaxTurn int `json:"max_turn,omitempty"`
}

// TriggerCondition 坑点触发条件：时机 + 概率 + 前置状态。
type TriggerCondition struct {
	// When 触发时机，缺省归一为 always。
	When TriggerWhen `json:"when"`
	// Probability 时机满足后真正注入的概率，[0,1]；1=确定性（T3-9 验收"符合条件100%出现"用 1）。
	Probability float64 `json:"probability"`
	// MaxTimes 同一坑点在一次训练内最多注入次数，缺省 1。
	MaxTimes int `json:"max_times,omitempty"`

	// question_match 用：命中任一关键词或正则模式即视为时机到来（正则在 T3-9 编译）。
	QuestionKeywords []string `json:"question_keywords,omitempty"`
	QuestionPatterns []string `json:"question_patterns,omitempty"`
	// stage_enter 用：排障阶段标识（如 detect/locate/fix，与编排层阶段口径对齐）。
	Stages []string `json:"stages,omitempty"`
	// delay 用：延迟秒数或时间窗口（二选一，窗口优先）。
	DelaySeconds int         `json:"delay_seconds,omitempty"`
	Window       *TimeWindow `json:"window,omitempty"`

	// Requires 前置状态。
	Requires *TriggerRequires `json:"requires,omitempty"`
}

// EffectiveProbability 缺省概率归一为 1（规则版坑点默认确定性触发）。
func (t TriggerCondition) EffectiveProbability() float64 {
	if t.Probability <= 0 {
		return 1
	}
	return t.Probability
}

// EffectiveMaxTimes 缺省最多注入一次。
func (t TriggerCondition) EffectiveMaxTimes() int {
	if t.MaxTimes <= 0 {
		return 1
	}
	return t.MaxTimes
}

// EffectiveWhen 缺省时机归一为 always。
func (t TriggerCondition) EffectiveWhen() TriggerWhen {
	if t.When == "" {
		return WhenAlways
	}
	return t.When
}

// RequiresFaultTriggered 前置是否要求故障已触发；未配置默认 true。
func (t TriggerCondition) RequiresFaultTriggered() bool {
	if t.Requires == nil || t.Requires.FaultTriggered == nil {
		return true
	}
	return *t.Requires.FaultTriggered
}

// RecognitionRules 识别判定规则（T3-11 消费；T3-8 仅解析校验其存在性）。
// 用户出现以下任一行为即算"识别了坑"。
type RecognitionRules struct {
	// DisputeKeywords 用户在回复中反驳/质疑（"不对/为什么/证据呢"等）。
	DisputeKeywords []string `json:"dispute_keywords,omitempty"`
	// IndependentVerify 用户去查了其它证据方向（查看指标/日志/终端命令的类别）。
	IndependentVerify []string `json:"independent_verify_actions,omitempty"`
	// IgnoreAndProceed 不采纳该建议、继续沿其它方向排查。
	IgnoreAndProceed bool `json:"ignore_and_proceed,omitempty"`
}

// FallRules 踩坑判定规则（T3-11 消费）。
type FallRules struct {
	// AdoptActions 用户采纳错误建议的行为（执行建议命令/按建议改配置/明确认同）。
	AdoptActions []string `json:"adopt_actions,omitempty"`
	// AdoptKeywords 用户回复中表达采纳（"好的/就按你说的/我执行了"等）。
	AdoptKeywords []string `json:"adopt_keywords,omitempty"`
	// WorsenFault 采纳后故障是否进一步恶化（T3-12 后果传导开关）。
	WorsenFault bool `json:"worsen_fault,omitempty"`
	// Consequence 采纳错误建议后联动注入的次生故障规格（T3-12）；
	// 为空且 WorsenFault=true 时默认注入 CPU 满载，强度按 Severity 归一。
	Consequence *ConsequenceFault `json:"consequence,omitempty"`
}
