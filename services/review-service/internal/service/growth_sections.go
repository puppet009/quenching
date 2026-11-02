package service

import (
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"strings"
)

func BuildGrowthSections(id int64, report rc.GrowthReport) ([]model.ReviewSection, error) {
	keys := []string{"root_cause", "timeline", "path_comparison", "highlights_pitfalls", "methodology", "knowledge"}
	types := []string{model.SectionTypeRootCauseAnalysis, model.SectionTypeFaultTimeline, model.SectionTypePathComparison, model.SectionTypeHighlightsPitfalls, model.SectionTypeMethodologySummary, model.SectionTypeKnowledgeExtension}
	titles := []string{"根因分析", "整场时间线", "排查路径对比", "亮点与坑点", "方法论提炼", "知识与行业拓展"}
	labels := map[string]string{"observed": "平台记录", "user_statement": "学员自述", "assistant_suggestion": "助手建议", "hypothesis": "待验证推断", "industry_advice": "行业建议"}
	var out []model.ReviewSection
	for i, key := range keys {
		var lines []string
		for _, c := range report.Claims {
			if c.Section == key {
				label, ok := labels[c.Category]
				if !ok {
					return nil, fmt.Errorf("unknown claim category")
				}
				lines = append(lines, fmt.Sprintf("**%s**：%s\n\n来源：%s", label, c.Text, strings.Join(c.SourceRefs, "、")))
			}
		}
		text := strings.Join(lines, "\n\n")
		if text == "" {
			text = "本场未获得足够证据，暂不作结论。"
		}
		out = append(out, model.ReviewSection{ReviewID: id, SectionType: types[i], SectionLevel: model.ReviewLevelMethod, Title: titles[i], Content: rc.RedactText(text), SortOrder: int16((i + 1) * 10)})
	}
	return out, nil
}
