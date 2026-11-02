// T3-13 坑点复盘章节单元测试。
package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
)

// ---------- PitfallItem.IsRecognized 测试 ----------

func TestPitfallItemRecognized(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected bool
	}{
		{"踩坑 agreed", model.PitfallResponseAgreed, false},
		{"识别 disputed", model.PitfallResponseDisputed, true},
		{"识别 verified", model.PitfallResponseVerified, true},
		{"识别 ignored", model.PitfallResponseIgnored, true},
		{"未响应 pending", model.PitfallResponsePending, false},
		{"空值", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &model.PitfallItem{UserResponse: tt.response}
			if got := item.IsRecognized(); got != tt.expected {
				t.Errorf("IsRecognized() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------- 标签函数测试 ----------

func TestPitfallTypeLabel(t *testing.T) {
	if model.PitfallTypeLabel(model.PitfallTypeCausalMisjudgment) != "因果误判" {
		t.Error("causal_misjudgment 标签错误")
	}
	if model.PitfallTypeLabel(model.PitfallTypeInfoOmission) != "信息遗漏" {
		t.Error("info_omission 标签错误")
	}
	if model.PitfallTypeLabel(model.PitfallTypeOverconfidence) != "过度自信" {
		t.Error("overconfidence 标签错误")
	}
}

func TestResponseLabel(t *testing.T) {
	if !strings.Contains(model.ResponseLabel(model.PitfallResponseAgreed), "踩坑") {
		t.Error("agreed 标签应包含'踩坑'")
	}
	if !strings.Contains(model.ResponseLabel(model.PitfallResponseDisputed), "识别") {
		t.Error("disputed 标签应包含'识别'")
	}
}

// ---------- BuildPitfallSection 测试 ----------

func TestBuildPitfallSectionNil(t *testing.T) {
	// nil 入参应返回 nil
	if got := service.BuildPitfallSection(1, nil); got != nil {
		t.Error("nil analysis 应返回 nil")
	}
}

func TestBuildPitfallSectionEmpty(t *testing.T) {
	// 无坑点应返回 nil
	analysis := &model.PitfallAnalysis{TotalCount: 0, Pitfalls: []model.PitfallItem{}}
	if got := service.BuildPitfallSection(1, analysis); got != nil {
		t.Error("空坑点应返回 nil")
	}
}

func TestBuildPitfallSectionWithPitfalls(t *testing.T) {
	analysis := &model.PitfallAnalysis{
		TotalCount:      3,
		RecognizedCount: 2,
		RecognitionRate: 66.7,
		Pitfalls: []model.PitfallItem{
			{
				PitfallConfigID: 101,
				PitfallType:     model.PitfallTypeCausalMisjudgment,
				Name:            "CPU 占用率误判",
				Description:     "容易把 CPU 高误判为应用问题，实际是系统层面。",
				Severity:        "high",
				ErrorContent:    "建议你直接重启应用服务，CPU 就下来了。",
				CorrectApproach: "先用 top/mpstat 看是用户态还是系统态，再定位具体进程。",
				UserResponse:    model.PitfallResponseDisputed,
				Recognized:      true,
				MessageID:       5001,
				AppearOffsetSec: 120,
			},
			{
				PitfallConfigID: 102,
				PitfallType:     model.PitfallTypeInfoOmission,
				Name:            "忽略慢查询日志",
				Description:     "只看错误日志，忽略了慢查询日志中的关键线索。",
				Severity:        "medium",
				ErrorContent:    "错误日志里没东西，应该是网络问题。",
				CorrectApproach: "排查数据库问题时，慢查询日志往往比错误日志更有价值。",
				UserResponse:    model.PitfallResponseAgreed,
				Recognized:      false,
				MessageID:       5002,
				AppearOffsetSec: 300,
			},
			{
				PitfallConfigID: 103,
				PitfallType:     model.PitfallTypeOverconfidence,
				Name:            "笃定的错误答案",
				Description:     "AI 给出非常笃定但完全错误的结论。",
				Severity:        "high",
				ErrorContent:    "100% 确定是内存泄漏导致的，赶紧加内存。",
				CorrectApproach: "对高置信度结论保持警惕，要求给出依据和推理过程。",
				UserResponse:    model.PitfallResponseVerified,
				Recognized:      true,
				MessageID:       5003,
				AppearOffsetSec: 480,
			},
		},
	}

	section := service.BuildPitfallSection(42, analysis)
	if section == nil {
		t.Fatal("有坑点时不应返回 nil")
	}

	// 校验基本字段
	if section.ReviewID != 42 {
		t.Errorf("ReviewID 应为 42，got %d", section.ReviewID)
	}
	if section.SectionType != model.SectionTypeFixAIPitfallAnalysis {
		t.Errorf("SectionType 应为 %s，got %s", model.SectionTypeFixAIPitfallAnalysis, section.SectionType)
	}
	if section.Title != model.PitfallSectionTitle {
		t.Errorf("Title 应为 %s，got %s", model.PitfallSectionTitle, section.Title)
	}
	if section.SortOrder != model.PitfallSectionOrder {
		t.Errorf("SortOrder 应为 %d，got %d", model.PitfallSectionOrder, section.SortOrder)
	}
	if section.SectionLevel != model.PitfallSectionLevel {
		t.Errorf("SectionLevel 应为 %s，got %s", model.PitfallSectionLevel, section.SectionLevel)
	}

	// 校验 Markdown 内容
	content := section.Content
	if content == "" {
		t.Fatal("Markdown 内容不应为空")
	}
	if !strings.Contains(content, "AI 坑点分析") {
		t.Error("Markdown 应包含章节标题")
	}
	if !strings.Contains(content, "3") || !strings.Contains(content, "2") {
		t.Error("Markdown 应包含坑点总数和识别数")
	}
	if !strings.Contains(content, "CPU 占用率误判") {
		t.Error("Markdown 应包含第一个坑点名称")
	}
	if !strings.Contains(content, "忽略慢查询日志") {
		t.Error("Markdown 应包含第二个坑点名称")
	}

	// 校验踩坑和识别的不同文案
	if !strings.Contains(content, "⚠️") {
		t.Error("踩坑的坑点应显示 ⚠️ 图标")
	}
	if !strings.Contains(content, "✅") {
		t.Error("识别的坑点应显示 ✅ 图标")
	}

	// 校验结构化数据
	if len(section.ContentData) == 0 {
		t.Fatal("content_data 不应为空")
	}
	var parsed model.PitfallAnalysis
	if err := json.Unmarshal(section.ContentData, &parsed); err != nil {
		t.Fatalf("解析 content_data 失败: %v", err)
	}
	if parsed.TotalCount != 3 {
		t.Errorf("结构化数据 TotalCount 应为 3，got %d", parsed.TotalCount)
	}
	if parsed.RecognizedCount != 2 {
		t.Errorf("结构化数据 RecognizedCount 应为 2，got %d", parsed.RecognizedCount)
	}
	if len(parsed.Pitfalls) != 3 {
		t.Errorf("结构化数据 Pitfalls 应有 3 项，got %d", len(parsed.Pitfalls))
	}
}

// ---------- fake pitfall collector ----------

type fakePitfallCollector struct {
	section *model.ReviewSection
	err     error
}

func (f *fakePitfallCollector) CollectAndBuild(_ context.Context, reviewID, sessionID int64) (*model.ReviewSection, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.section != nil {
		// 设置 review_id（模拟真实行为）
		s := *f.section
		s.ReviewID = reviewID
		return &s, nil
	}
	return nil, nil
}

// ---------- GetDetail 服务测试 ----------

func TestGetDetailWithSections(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	// 先创建复盘 + 章节
	rv, _ := svc.StartGeneration(context.Background(), 1001, 101, 1)
	sections := []model.ReviewSection{
		{ReviewID: rv.ID, SectionType: model.SectionTypeRootCauseAnalysis, Title: "根因分析", Content: "根因内容", SortOrder: 10},
		{ReviewID: rv.ID, SectionType: model.SectionTypeFaultTimeline, Title: "时间线解读", Content: "时间线内容", SortOrder: 20},
	}
	_ = repo.ReplaceSections(context.Background(), rv.ID, sections)

	detail, err := svc.GetDetail(context.Background(), rv.ID)
	if err != nil {
		t.Fatalf("GetDetail 应成功，got err: %v", err)
	}

	if detail.SessionID != 1001 {
		t.Errorf("session_id 不匹配: %d", detail.SessionID)
	}
	if len(detail.Sections) != 2 {
		t.Errorf("应有 2 个章节，got %d", len(detail.Sections))
	}
	if detail.Sections[0].SectionType != model.SectionTypeRootCauseAnalysis {
		t.Errorf("第一章应为根因分析，got %s", detail.Sections[0].SectionType)
	}
}

func TestGetDetailNotFound(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	_, err := svc.GetDetail(context.Background(), 9999)
	if err == nil {
		t.Fatal("不存在的复盘应返回错误")
	}
	if err != service.ErrNotFound {
		t.Errorf("应返回 ErrNotFound，got %v", err)
	}
}

// ---------- GetSections 服务测试 ----------

func TestGetSections(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	rv, _ := svc.StartGeneration(context.Background(), 1001, 101, 1)
	sections := []model.ReviewSection{
		{ReviewID: rv.ID, SectionType: model.SectionTypeFixAIPitfallAnalysis, Title: "AI 坑点分析", Content: "坑点内容", SortOrder: 30},
	}
	_ = repo.ReplaceSections(context.Background(), rv.ID, sections)

	result, err := svc.GetSections(context.Background(), rv.ID)
	if err != nil {
		t.Fatalf("GetSections 应成功，got err: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("应有 1 个章节，got %d", len(result))
	}
	if result[0].SectionType != model.SectionTypeFixAIPitfallAnalysis {
		t.Errorf("章节类型应为 %s，got %s", model.SectionTypeFixAIPitfallAnalysis, result[0].SectionType)
	}
}

// ---------- 总体改进建议测试 ----------

func TestOverallSuggestionsHighRate(t *testing.T) {
	analysis := &model.PitfallAnalysis{
		TotalCount:      5,
		RecognizedCount: 5,
		RecognitionRate: 100,
		Pitfalls: []model.PitfallItem{
			{Name: "测试坑点", UserResponse: model.PitfallResponseVerified, Recognized: true},
		},
	}
	section := service.BuildPitfallSection(1, analysis)
	if section == nil {
		t.Fatal("不应返回 nil")
	}
	if !strings.Contains(section.Content, "表现优秀") {
		t.Error("高识别率应显示'表现优秀'")
	}
}

func TestOverallSuggestionsLowRate(t *testing.T) {
	analysis := &model.PitfallAnalysis{
		TotalCount:      4,
		RecognizedCount: 1,
		RecognitionRate: 25,
		Pitfalls: []model.PitfallItem{
			{Name: "坑1", UserResponse: model.PitfallResponseAgreed, Recognized: false,
				PitfallType: model.PitfallTypeCausalMisjudgment},
			{Name: "坑2", UserResponse: model.PitfallResponseAgreed, Recognized: false,
				PitfallType: model.PitfallTypeOverconfidence},
			{Name: "坑3", UserResponse: model.PitfallResponseAgreed, Recognized: false,
				PitfallType: model.PitfallTypeInfoOmission},
			{Name: "坑4", UserResponse: model.PitfallResponseVerified, Recognized: true},
		},
	}
	section := service.BuildPitfallSection(1, analysis)
	if section == nil {
		t.Fatal("不应返回 nil")
	}
	if !strings.Contains(section.Content, "需要加强") {
		t.Error("低识别率应显示'需要加强'")
	}
}

// ---------- 按类型分布表格测试 ----------

func TestPitfallTypeDistribution(t *testing.T) {
	analysis := &model.PitfallAnalysis{
		TotalCount:      4,
		RecognizedCount: 3,
		RecognitionRate: 75,
		Pitfalls: []model.PitfallItem{
			{PitfallType: model.PitfallTypeCausalMisjudgment, Recognized: true},
			{PitfallType: model.PitfallTypeCausalMisjudgment, Recognized: true},
			{PitfallType: model.PitfallTypeInfoOmission, Recognized: false},
			{PitfallType: model.PitfallTypeOverconfidence, Recognized: true},
		},
	}
	section := service.BuildPitfallSection(1, analysis)
	if section == nil {
		t.Fatal("不应返回 nil")
	}
	content := section.Content
	if !strings.Contains(content, "按类型分布") {
		t.Error("应包含按类型分布表格")
	}
	if !strings.Contains(content, "因果误判") {
		t.Error("表格应包含因果误判类型")
	}
	if !strings.Contains(content, "信息遗漏") {
		t.Error("表格应包含信息遗漏类型")
	}
	if !strings.Contains(content, "过度自信") {
		t.Error("表格应包含过度自信类型")
	}
}
