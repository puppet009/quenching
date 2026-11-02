// 复盘章节结构化模型（T2-29 基础复盘 / T3-1 方法论复盘）。
//
// 基础复盘（review_level=basic）的验收口径是两章：
//   - 根因分析 RootCause：故障传播路径、关键节点、根因判断；
//   - 时间线解读 Timeline：用户操作时间线的重述与关键拐点标注。
//
// 方法论复盘（review_level=method）在两章之外再加四章（T3-1 + T3-2，共六章）：
//   - 路径对比 PathComparison（T3-1）：用户实际路径 vs 专家最优路径，标注弯路点与浪费时间；
//   - 亮点坑点 HighlightsPitfalls（T3-2）：做得好的地方 + 典型错误操作及为什么是错的；
//   - 方法论 Methodology（T3-2）：该类故障可迁移的通用排查框架；
//   - 知识点 Knowledge（T3-2）：核心技术概念解释与延伸阅读。
//
// 注意：ai-service 六段式中的 pitfall_analysis 是"AI 挖坑/踩坑逐点明细"，
// 语义上属于 T3-13 的逐坑点分析章节，本文件的亮点坑点只做基于时间线的概述，不消费该键；
// collab_review（协作评估）留待协作评估相关任务卡消费。
// T2-30 起，章节经 model.BasicSections / model.MethodSections 映射后写入 review_sections。

package model

// SectionRootCause 根因分析章节的 JSON 键（与 ai-service sections_json 对齐）。
const SectionRootCause = "root_cause"

// SectionTimeline 时间线解读章节的 JSON 键。
const SectionTimeline = "timeline"

// SectionPathComparison 路径对比章节的 JSON 键（T3-1，与 ai-service 六段式对齐）。
const SectionPathComparison = "path_comparison"

// SectionHighlightsPitfalls 亮点坑点章节的 JSON 键（T3-2）。
// 注意与 ai-service 的 pitfall_analysis 区分：本键总结"用户操作的亮点与典型错误"，
// pitfall_analysis 是"AI 挖坑/踩坑逐点明细"（T3-13 消费）。
const SectionHighlightsPitfalls = "highlights_pitfalls"

// SectionMethodology 方法论提炼章节的 JSON 键（T3-2，与 ai-service methodology 对齐）。
const SectionMethodology = "methodology"

// SectionKnowledge 知识点梳理章节的 JSON 键（T3-2）。
const SectionKnowledge = "knowledge"

// ReviewSections 复盘结构化章节。
//
// RootCause/Timeline 两章为基础复盘与方法论复盘共有；
// PathComparison 仅方法论复盘（T3-1）必填，基础复盘留空、不消费。
type ReviewSections struct {
	// RootCause 根因分析：基于故障剧本与指标快照给出的根因链路解读。
	RootCause string `json:"root_cause"`
	// Timeline 时间线解读：操作时间线重述与关键节点标注。
	Timeline string `json:"timeline"`
	// PathComparison 路径对比：用户实际路径 vs 专家最优路径，标注弯路与时间浪费（T3-1）。
	PathComparison string `json:"path_comparison,omitempty"`
	// HighlightsPitfalls 亮点坑点：做得好的地方 + 典型错误操作及为什么是错的（T3-2）。
	HighlightsPitfalls string `json:"highlights_pitfalls,omitempty"`
	// Methodology 方法论提炼：该类故障可迁移的通用排查框架（T3-2）。
	Methodology string `json:"methodology,omitempty"`
	// Knowledge 知识点梳理：核心技术概念解释与延伸阅读（T3-2）。
	Knowledge string `json:"knowledge,omitempty"`
}

// Valid 基础复盘口径：两章均非空才算生成成功；任一章缺失视为 AI 输出不合格，应重试/兜底。
func (s *ReviewSections) Valid() bool {
	return s != nil && s.RootCause != "" && s.Timeline != ""
}

// MethodValid 方法论复盘口径（T3-1）：根因/时间线/路径对比三章均非空。
// 保留三章口径用于 T3-1 既有防御与回归；T3-2 起的完整口径见 MethodFullValid。
func (s *ReviewSections) MethodValid() bool {
	return s != nil && s.RootCause != "" && s.Timeline != "" && s.PathComparison != ""
}

// MethodFullValid 完整方法论复盘口径（T3-2）：六章均非空才算生成成功。
func (s *ReviewSections) MethodFullValid() bool {
	return s != nil && s.RootCause != "" && s.Timeline != "" && s.PathComparison != "" &&
		s.HighlightsPitfalls != "" && s.Methodology != "" && s.Knowledge != ""
}
