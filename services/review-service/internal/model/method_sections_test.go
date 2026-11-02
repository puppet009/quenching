package model

import "testing"

func TestMethodValid(t *testing.T) {
	cases := []struct {
		name string
		s    *ReviewSections
		want bool
	}{
		{"nil", nil, false},
		{"三章齐全", &ReviewSections{RootCause: "r", Timeline: "t", PathComparison: "p"}, true},
		{"缺路径对比", &ReviewSections{RootCause: "r", Timeline: "t"}, false},
		{"缺根因", &ReviewSections{Timeline: "t", PathComparison: "p"}, false},
		{"缺时间线", &ReviewSections{RootCause: "r", PathComparison: "p"}, false},
		// 只有 T3-1 前三章、缺 T3-2 三章时，三章口径仍成立（MethodValid 为 T3-1 保留）。
		{"仅前三章也算三章齐全", &ReviewSections{RootCause: "r", Timeline: "t", PathComparison: "p"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.MethodValid(); got != tc.want {
				t.Errorf("MethodValid() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMethodFullValid(t *testing.T) {
	full := func() *ReviewSections {
		return &ReviewSections{
			RootCause: "r", Timeline: "t", PathComparison: "p",
			HighlightsPitfalls: "h", Methodology: "m", Knowledge: "k",
		}
	}
	cases := []struct {
		name string
		s    *ReviewSections
		want bool
	}{
		{"nil", nil, false},
		{"六章齐全", full(), true},
		{"仅 T3-1 前三章", &ReviewSections{RootCause: "r", Timeline: "t", PathComparison: "p"}, false},
		{"缺亮点坑点", func() *ReviewSections { s := full(); s.HighlightsPitfalls = ""; return s }(), false},
		{"缺方法论", func() *ReviewSections { s := full(); s.Methodology = ""; return s }(), false},
		{"缺知识点", func() *ReviewSections { s := full(); s.Knowledge = ""; return s }(), false},
		{"缺路径对比", func() *ReviewSections { s := full(); s.PathComparison = ""; return s }(), false},
		{"缺根因", func() *ReviewSections { s := full(); s.RootCause = ""; return s }(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.MethodFullValid(); got != tc.want {
				t.Errorf("MethodFullValid() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMethodSections_Mapping(t *testing.T) {
	s := &ReviewSections{
		RootCause:          "根因正文",
		Timeline:           "时间线正文",
		PathComparison:     "路径对比正文",
		HighlightsPitfalls: "亮点坑点正文",
		Methodology:        "方法论正文",
		Knowledge:          "知识点正文",
	}

	sections := MethodSections(5005, s)
	if len(sections) != 6 {
		t.Fatalf("应映射 6 章，实际 %d", len(sections))
	}

	want := []struct {
		typ     string
		title   string
		order   int16
		content string
	}{
		{SectionTypeRootCauseAnalysis, "根因分析", 10, "根因正文"},
		{SectionTypeFaultTimeline, "时间线解读", 20, "时间线正文"},
		{SectionTypePathComparison, "路径对比", 30, "路径对比正文"},
		{SectionTypeHighlightsPitfalls, "亮点与坑点", 40, "亮点坑点正文"},
		{SectionTypeMethodologySummary, "方法论提炼", 50, "方法论正文"},
		{SectionTypeKnowledgeExtension, "知识点梳理", 60, "知识点正文"},
	}
	for i, w := range want {
		got := sections[i]
		if got.ReviewID != 5005 {
			t.Errorf("第%d章 ReviewID = %d, want 5005", i, got.ReviewID)
		}
		if got.SectionType != w.typ {
			t.Errorf("第%d章 SectionType = %q, want %q", i, got.SectionType, w.typ)
		}
		if got.SectionLevel != SectionLevelMethod {
			t.Errorf("第%d章 SectionLevel = %q, want method", i, got.SectionLevel)
		}
		if got.Title != w.title {
			t.Errorf("第%d章 Title = %q, want %q", i, got.Title, w.title)
		}
		if got.SortOrder != w.order {
			t.Errorf("第%d章 SortOrder = %d, want %d", i, got.SortOrder, w.order)
		}
		if got.Content != w.content {
			t.Errorf("第%d章 Content = %q, want %q", i, got.Content, w.content)
		}
	}

	// 路径对比 / 亮点坑点类型取值须与 ai-service JSON 键对齐。
	if SectionTypePathComparison != SectionPathComparison {
		t.Errorf("落库类型 %q 应与 JSON 键 %q 一致", SectionTypePathComparison, SectionPathComparison)
	}
	if SectionTypeHighlightsPitfalls != SectionHighlightsPitfalls {
		t.Errorf("落库类型 %q 应与 JSON 键 %q 一致", SectionTypeHighlightsPitfalls, SectionHighlightsPitfalls)
	}
}

func TestMethodSections_NilAndIncomplete(t *testing.T) {
	cases := []struct {
		name string
		s    *ReviewSections
	}{
		{"nil", nil},
		{"仅 T3-1 前三章", &ReviewSections{RootCause: "r", Timeline: "t", PathComparison: "p"}},
		{"缺根因", &ReviewSections{Timeline: "t", PathComparison: "p", HighlightsPitfalls: "h", Methodology: "m", Knowledge: "k"}},
		{"缺时间线", &ReviewSections{RootCause: "r", PathComparison: "p", HighlightsPitfalls: "h", Methodology: "m", Knowledge: "k"}},
		{"缺路径对比", &ReviewSections{RootCause: "r", Timeline: "t", HighlightsPitfalls: "h", Methodology: "m", Knowledge: "k"}},
		{"缺亮点坑点", &ReviewSections{RootCause: "r", Timeline: "t", PathComparison: "p", Methodology: "m", Knowledge: "k"}},
		{"缺方法论", &ReviewSections{RootCause: "r", Timeline: "t", PathComparison: "p", HighlightsPitfalls: "h", Knowledge: "k"}},
		{"缺知识点", &ReviewSections{RootCause: "r", Timeline: "t", PathComparison: "p", HighlightsPitfalls: "h", Methodology: "m"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MethodSections(1, tc.s); got != nil {
				t.Errorf("不完整输入应返回 nil，实际 %d 章", len(got))
			}
		})
	}
}
