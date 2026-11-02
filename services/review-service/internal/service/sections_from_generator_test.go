package service

import (
	"context"
	"testing"

	"github.com/quenching/review-service/internal/model"
)

// basicOnlyGenerator 仅实现锁定的七返回值端口（模拟基础生成器 / T2-31 的 D2 fake），
// 不实现 MethodSectionsGenerator。
type basicOnlyGenerator struct {
	summary string
	overall string
}

func (g *basicOnlyGenerator) Generate(_ context.Context, _ *ReviewTask) (
	title, summary, overall string, techScore, collabScore float64, durationMs int, err error,
) {
	return "标题", g.summary, g.overall, 0, 0, 0, nil
}

// methodGeneratorFake 同时实现两个端口，methodSections 可控（含返回 nil 的情形）。
type methodGeneratorFake struct {
	basicOnlyGenerator
	methodSections *model.ReviewSections
}

func (g *methodGeneratorFake) MethodSections() *model.ReviewSections {
	return g.methodSections
}

// 编译期断言两个 fake 的端口归属。
var (
	_ ReviewGenerator         = (*basicOnlyGenerator)(nil)
	_ MethodSectionsGenerator = (*methodGeneratorFake)(nil)
)

func TestSectionsFromGenerator_BasicGeneratorFallsBackToTwoSections(t *testing.T) {
	gen := &basicOnlyGenerator{summary: "根因正文", overall: "时间线正文"}

	sections := sectionsFromGenerator(gen, 1001, "根因正文", "时间线正文")
	if len(sections) != 2 {
		t.Fatalf("基础生成器应落 2 章，实际 %d", len(sections))
	}
	assertSection(t, sections[0], model.SectionTypeRootCauseAnalysis, model.SectionLevelBasic, "根因分析", 10, "根因正文")
	assertSection(t, sections[1], model.SectionTypeFaultTimeline, model.SectionLevelBasic, "时间线解读", 20, "时间线正文")
	for _, s := range sections {
		if s.ReviewID != 1001 {
			t.Errorf("ReviewID = %d, want 1001", s.ReviewID)
		}
	}
}

func TestSectionsFromGenerator_MethodGeneratorReturnsSixSections(t *testing.T) {
	gen := &methodGeneratorFake{
		methodSections: &model.ReviewSections{
			RootCause:          "根因正文",
			Timeline:           "时间线正文",
			PathComparison:     "路径对比正文",
			HighlightsPitfalls: "亮点坑点正文",
			Methodology:        "方法论正文",
			Knowledge:          "知识点正文",
		},
	}

	sections := sectionsFromGenerator(gen, 2002, "根因正文", "时间线正文")
	if len(sections) != 6 {
		t.Fatalf("方法级生成器应落 6 章，实际 %d", len(sections))
	}
	assertSection(t, sections[0], model.SectionTypeRootCauseAnalysis, model.SectionLevelMethod, "根因分析", 10, "根因正文")
	assertSection(t, sections[1], model.SectionTypeFaultTimeline, model.SectionLevelMethod, "时间线解读", 20, "时间线正文")
	assertSection(t, sections[2], model.SectionTypePathComparison, model.SectionLevelMethod, "路径对比", 30, "路径对比正文")
	assertSection(t, sections[3], model.SectionTypeHighlightsPitfalls, model.SectionLevelMethod, "亮点与坑点", 40, "亮点坑点正文")
	assertSection(t, sections[4], model.SectionTypeMethodologySummary, model.SectionLevelMethod, "方法论提炼", 50, "方法论正文")
	assertSection(t, sections[5], model.SectionTypeKnowledgeExtension, model.SectionLevelMethod, "知识点梳理", 60, "知识点正文")
}

// 非 nil 但只有 T3-1 三章（不满足六章防御）：不能静默降级成基础两章，
// 应返回 nil，由调用方按 ErrEmptySections fail-closed 重试。
func TestSectionsFromGenerator_PartialMethodSectionsReturnsNil(t *testing.T) {
	gen := &methodGeneratorFake{
		methodSections: &model.ReviewSections{
			RootCause:      "根因正文",
			Timeline:       "时间线正文",
			PathComparison: "路径对比正文",
		},
	}

	if sections := sectionsFromGenerator(gen, 2003, "根因正文", "时间线正文"); sections != nil {
		t.Fatalf("不完整方法章节应返回 nil 触发重试，实际 %d 章", len(sections))
	}
}

func TestSectionsFromGenerator_NilMethodSectionsFallsBack(t *testing.T) {
	// 实现了扩展端口但本次返回 nil（失败 Generate 后的契约状态）：
	// 必须退回基础两章，不能产出半成品方法级章节。
	gen := &methodGeneratorFake{methodSections: nil}

	sections := sectionsFromGenerator(gen, 3003, "根因正文", "时间线正文")
	if len(sections) != 2 {
		t.Fatalf("nil 方法章节应回退 2 章，实际 %d", len(sections))
	}
	if sections[0].SectionLevel != model.SectionLevelBasic || sections[1].SectionLevel != model.SectionLevelBasic {
		t.Errorf("回退章节 level 应为 basic: %+v", sections)
	}
}

func assertSection(t *testing.T, s model.ReviewSection, sectionType, level, title string, order int16, content string) {
	t.Helper()
	if s.SectionType != sectionType {
		t.Errorf("SectionType = %q, want %q", s.SectionType, sectionType)
	}
	if s.SectionLevel != level {
		t.Errorf("SectionLevel = %q, want %q", s.SectionLevel, level)
	}
	if s.Title != title {
		t.Errorf("Title = %q, want %q", s.Title, title)
	}
	if s.SortOrder != order {
		t.Errorf("SortOrder = %d, want %d", s.SortOrder, order)
	}
	if s.Content != content {
		t.Errorf("Content = %q, want %q", s.Content, content)
	}
}
