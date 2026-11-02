// 复盘章节明细（T2-30，《数据库设计文档》§10.2，review_sections 表）。
//
// 一份复盘按章节存储（基础复盘两章，方法论版起扩展到多章），
// section_type 走固定词表；content 存 Markdown 正文，content_data
// 预留给后续结构化（卡片/表格）载荷，基础复盘为 nil。

package model

import "time"

// 章节类型词表（§10.2）。基础复盘只落前两章，其余随方法论版/架构版启用。
const (
	SectionTypeFaultTimeline          = "fault_timeline"          // 故障时间线
	SectionTypeRootCauseAnalysis      = "root_cause_analysis"     // 根因分析
	SectionTypeFixAIPitfallAnalysis   = "fix_ai_pitfall_analysis" // 修复 AI 坑点分析
	SectionTypeKnowledgeExtension     = "knowledge_extension"     // 知识拓展
	SectionTypeArchitectureAnalysis   = "architecture_analysis"   // 架构分析
	SectionTypeInterviewQuestions     = "interview_questions"     // 面试题
	SectionTypeMethodologySummary     = "methodology_summary"     // 方法论总结
	SectionTypeImprovementSuggestions = "improvement_suggestions" // 改进建议
)

// SectionTypePathComparison 路径对比章节类型（T3-1）。
//
// §10.2 初版词表未单列路径对比；此处与 ai-service sections_json 的键保持一致
// （path_comparison），作为方法论复盘专属章节取值，后续词表回填时一并对齐。
const SectionTypePathComparison = "path_comparison" // 排查路径对比（实际 vs 最优）

// SectionTypeHighlightsPitfalls 亮点坑点章节类型（T3-2）。
//
// §10.2 初版词表未单列亮点坑点；与 ai-service sections_json 新增键保持一致
// （highlights_pitfalls），作为方法论复盘专属章节取值，后续词表回填时一并对齐。
// 注意与 fix_ai_pitfall_analysis 区分：后者是 AI 挖坑/踩坑逐点明细（T3-13）。
const SectionTypeHighlightsPitfalls = "highlights_pitfalls" // 亮点与坑点（基于时间线的概述）

// 章节级别，与 reviews.review_level 对齐。
const (
	SectionLevelBasic  = "basic"
	SectionLevelMethod = "method"
	SectionLevelArch   = "arch"
)

// 基础复盘两章的固定标题与排序（报告页 T2-32 直接展示）。
const (
	basicRootCauseOrder = 10
	basicTimelineOrder  = 20

	basicRootCauseTitle = "根因分析"
	basicTimelineTitle  = "时间线解读"
)

// 方法论复盘章节的固定标题与排序（T3-1 路径对比，T3-2 再扩三章）。
// 根因/时间线复用 basic 的 order=10/20，路径对比 30、亮点坑点 40、方法论 50、知识点 60。
const (
	methodPathComparisonOrder = 30
	methodHighlightsOrder     = 40
	methodMethodologyOrder    = 50
	methodKnowledgeOrder      = 60

	methodPathComparisonTitle = "路径对比"
	methodHighlightsTitle     = "亮点与坑点"
	methodMethodologyTitle    = "方法论提炼"
	methodKnowledgeTitle      = "知识点梳理"
)

// ReviewSection 复盘章节明细，对应 review_sections 一行。
type ReviewSection struct {
	ID           int64     `json:"id"`
	ReviewID     int64     `json:"review_id"`
	SectionType  string    `json:"section_type"`
	SectionLevel string    `json:"section_level"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	ContentData  []byte    `json:"content_data,omitempty"` // JSONB，基础复盘为空
	SortOrder    int16     `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// BasicSections 把基础复盘的两章结构转换为待落库的章节明细。
//
// 映射（T2-29 JSON 键 → §10.2 章节词表）：
//   - root_cause → root_cause_analysis（根因分析，sort_order=10）
//   - timeline   → fault_timeline（时间线解读，sort_order=20）
//
// 调用前应已通过 ReviewSections.Valid() 校验（两章非空）；
// 此处再做防御性校验，任一为空返回 nil 由上层按生成失败重试处理，
// 避免半成品章节落库。
func BasicSections(reviewID int64, s *ReviewSections) []ReviewSection {
	if s == nil || s.RootCause == "" || s.Timeline == "" {
		return nil
	}
	return []ReviewSection{
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypeRootCauseAnalysis,
			SectionLevel: SectionLevelBasic,
			Title:        basicRootCauseTitle,
			Content:      s.RootCause,
			SortOrder:    basicRootCauseOrder,
		},
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypeFaultTimeline,
			SectionLevel: SectionLevelBasic,
			Title:        basicTimelineTitle,
			Content:      s.Timeline,
			SortOrder:    basicTimelineOrder,
		},
	}
}

// MethodSections 把方法论复盘的六章结构转换为待落库的章节明细（T3-1 三章 + T3-2 三章）。
//
// 映射（JSON 键 → §10.2 章节词表/专属取值）：
//   - root_cause           → root_cause_analysis（根因分析，sort_order=10，level=method）
//   - timeline             → fault_timeline（时间线解读，sort_order=20，level=method）
//   - path_comparison      → path_comparison（路径对比，sort_order=30，level=method）
//   - highlights_pitfalls  → highlights_pitfalls（亮点与坑点，sort_order=40，level=method）
//   - methodology          → methodology_summary（方法论提炼，sort_order=50，level=method）
//   - knowledge            → knowledge_extension（知识点梳理，sort_order=60，level=method）
//
// 调用前应已通过 ReviewSections.MethodFullValid() 校验（六章非空）；
// 此处再做防御性校验，任一为空返回 nil 由上层按生成失败重试处理，
// 避免半成品章节落库。六章 section_level 均为 method，与 reviews.review_level 对齐。
func MethodSections(reviewID int64, s *ReviewSections) []ReviewSection {
	if s == nil || s.RootCause == "" || s.Timeline == "" || s.PathComparison == "" ||
		s.HighlightsPitfalls == "" || s.Methodology == "" || s.Knowledge == "" {
		return nil
	}
	return []ReviewSection{
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypeRootCauseAnalysis,
			SectionLevel: SectionLevelMethod,
			Title:        basicRootCauseTitle,
			Content:      s.RootCause,
			SortOrder:    basicRootCauseOrder,
		},
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypeFaultTimeline,
			SectionLevel: SectionLevelMethod,
			Title:        basicTimelineTitle,
			Content:      s.Timeline,
			SortOrder:    basicTimelineOrder,
		},
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypePathComparison,
			SectionLevel: SectionLevelMethod,
			Title:        methodPathComparisonTitle,
			Content:      s.PathComparison,
			SortOrder:    methodPathComparisonOrder,
		},
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypeHighlightsPitfalls,
			SectionLevel: SectionLevelMethod,
			Title:        methodHighlightsTitle,
			Content:      s.HighlightsPitfalls,
			SortOrder:    methodHighlightsOrder,
		},
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypeMethodologySummary,
			SectionLevel: SectionLevelMethod,
			Title:        methodMethodologyTitle,
			Content:      s.Methodology,
			SortOrder:    methodMethodologyOrder,
		},
		{
			ReviewID:     reviewID,
			SectionType:  SectionTypeKnowledgeExtension,
			SectionLevel: SectionLevelMethod,
			Title:        methodKnowledgeTitle,
			Content:      s.Knowledge,
			SortOrder:    methodKnowledgeOrder,
		},
	}
}
