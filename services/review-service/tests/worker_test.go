package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
)

// ---------- fake generator ----------

type fakeGenerator struct {
	fail      bool
	callCount int
}

func (f *fakeGenerator) Generate(_ context.Context, _ *service.ReviewTask) (string, string, string, float64, float64, int, error) {
	f.callCount++
	if f.fail {
		return "", "", "", 0, 0, 0, errors.New("ai service down")
	}
	return "复盘标题", "复盘摘要", "总体评价", 85.5, 78.0, 120000, nil
}

// ---------- ReviewTask 测试 ----------

func TestReviewTaskMarshalUnmarshal(t *testing.T) {
	task := &service.ReviewTask{
		ReviewID:  42,
		SessionID: 1001,
		UserID:    101,
		ProjectID: 1,
		TraceID:   "trace-test-001",
	}

	data, err := task.Marshal()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded service.ReviewTask
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.ReviewID != 42 {
		t.Errorf("ReviewID 不匹配: %d", decoded.ReviewID)
	}
	if decoded.SessionID != 1001 {
		t.Errorf("SessionID 不匹配: %d", decoded.SessionID)
	}
	if decoded.TraceID != "trace-test-001" {
		t.Errorf("TraceID 不匹配: %s", decoded.TraceID)
	}
}

// ---------- Worker 逻辑测试（非消费循环） ----------
// 我们不实际连 RabbitMQ 测消费循环，只测核心处理逻辑。

// TestWorkerSuccessPath 测试成功生成路径。
func TestWorkerSuccessPath(t *testing.T) {
	repo := newFakeReviewRepo()
	gen := &fakeGenerator{}

	// 先创建一个 pending 状态的复盘
	rv := &model.Review{
		ID:               1,
		SessionID:        1001,
		UserID:           101,
		ProjectID:        1,
		GenerationStatus: model.GenStatusPending,
	}
	repo.reviews[1] = rv

	// 模拟 worker 处理流程
	task := &service.ReviewTask{ReviewID: 1, SessionID: 1001}

	// 1. 标记 generating
	err := repo.MarkGenerating(context.Background(), 1)
	if err != nil {
		t.Fatalf("标记 generating 失败: %v", err)
	}
	if rv.GenerationStatus != model.GenStatusGenerating {
		t.Errorf("状态应为 generating，got %s", rv.GenerationStatus)
	}

	// 2. 调用 generator
	title, summary, overall, tech, collab, dur, err := gen.Generate(context.Background(), task)
	if err != nil {
		t.Fatalf("生成应成功，got err: %v", err)
	}

	// 3. 章节结构化并落库（先于 MarkCompleted）
	sections := model.BasicSections(1, &model.ReviewSections{RootCause: summary, Timeline: overall})
	if len(sections) == 0 {
		t.Fatal("章节不应为空")
	}
	if err := repo.ReplaceSections(context.Background(), 1, sections); err != nil {
		t.Fatalf("章节落库失败: %v", err)
	}

	// 4. 标记 completed
	err = repo.MarkCompleted(context.Background(), 1, title, summary, overall, dur, tech, collab, 0, 0)
	if err != nil {
		t.Fatalf("标记完成失败: %v", err)
	}

	if rv.GenerationStatus != model.GenStatusCompleted {
		t.Errorf("状态应为 completed，got %s", rv.GenerationStatus)
	}
	if rv.Title != "复盘标题" {
		t.Errorf("标题不匹配: %s", rv.Title)
	}
	if rv.TechScore != 85.5 {
		t.Errorf("技术分不匹配: %f", rv.TechScore)
	}
	if rv.GenerationDuration != 120000 {
		t.Errorf("耗时不匹配: %d", rv.GenerationDuration)
	}

	// 4. 章节结构化落库（T2-30）：根因/时间线两章。
	sections, ok := repo.sections[1]
	if !ok {
		t.Fatal("应已写入 review_sections")
	}
	if len(sections) != 2 {
		t.Fatalf("应有 2 个章节，got %d", len(sections))
	}
	if sections[0].SectionType != model.SectionTypeRootCauseAnalysis ||
		sections[0].Title != "根因分析" || sections[0].Content != "复盘摘要" {
		t.Errorf("根因章节映射错误: %+v", sections[0])
	}
	if sections[1].SectionType != model.SectionTypeFaultTimeline ||
		sections[1].Title != "时间线解读" || sections[1].Content != "总体评价" {
		t.Errorf("时间线章节映射错误: %+v", sections[1])
	}
	if sections[0].SortOrder >= sections[1].SortOrder {
		t.Errorf("章节顺序错误: root_cause(%d) 应在 timeline(%d) 前",
			sections[0].SortOrder, sections[1].SortOrder)
	}
	if sections[0].SectionLevel != model.SectionLevelBasic {
		t.Errorf("章节级别应为 basic，got %s", sections[0].SectionLevel)
	}

	// 5. 重放幂等：再次整章重写仍是 2 章，不产生重复行。
	rewritten := model.BasicSections(1, &model.ReviewSections{RootCause: "新根因", Timeline: "新时间线"})
	if err := repo.ReplaceSections(context.Background(), 1, rewritten); err != nil {
		t.Fatalf("重写章节失败: %v", err)
	}
	sections = repo.sections[1]
	if len(sections) != 2 {
		t.Fatalf("重写后仍应只有 2 个章节，got %d", len(sections))
	}
	if sections[0].Content != "新根因" {
		t.Errorf("重写后内容未更新: %s", sections[0].Content)
	}
}

// TestBasicSectionsMapping 测试两章结构化映射与空值拦截（纯模型逻辑）。
func TestBasicSectionsMapping(t *testing.T) {
	// 缺章节时返回 nil，由 worker 按失败重试处理。
	if got := model.BasicSections(1, &model.ReviewSections{RootCause: "只有根因"}); got != nil {
		t.Errorf("时间线缺失应返回 nil，got %+v", got)
	}
	if got := model.BasicSections(1, nil); got != nil {
		t.Errorf("nil 入参应返回 nil，got %+v", got)
	}

	got := model.BasicSections(7, &model.ReviewSections{RootCause: " 根因 ", Timeline: "时间线"})
	if len(got) != 2 {
		t.Fatalf("应映射 2 章，got %d", len(got))
	}
	for _, s := range got {
		if s.ReviewID != 7 {
			t.Errorf("ReviewID 应为 7，got %d", s.ReviewID)
		}
	}
}

// TestWorkerRetryPath 测试失败重试路径。
func TestWorkerRetryPath(t *testing.T) {
	repo := newFakeReviewRepo()
	gen := &fakeGenerator{fail: true}

	rv := &model.Review{
		ID:               1,
		SessionID:        1001,
		GenerationStatus: model.GenStatusGenerating,
	}
	repo.reviews[1] = rv

	// 第一次失败
	_, _, _, _, _, _, err := gen.Generate(context.Background(), &service.ReviewTask{ReviewID: 1})
	if err == nil {
		t.Fatal("应失败")
	}
	count, _ := repo.IncrementRetry(context.Background(), 1)
	if count != 1 {
		t.Errorf("第一次重试后 retry_count 应为 1，got %d", count)
	}

	// 第二次失败
	gen = &fakeGenerator{fail: true}
	_, _, _, _, _, _, err = gen.Generate(context.Background(), &service.ReviewTask{ReviewID: 1})
	count, _ = repo.IncrementRetry(context.Background(), 1)
	if count != 2 {
		t.Errorf("第二次重试后 retry_count 应为 2，got %d", count)
	}

	// 第三次失败（达到 MaxRetryCount = 3 次）
	gen = &fakeGenerator{fail: true}
	_, _, _, _, _, _, err = gen.Generate(context.Background(), &service.ReviewTask{ReviewID: 1})
	count, _ = repo.IncrementRetry(context.Background(), 1)
	if count != service.MaxRetryCount {
		t.Errorf("第 %d 次应达到最大重试次数 %d", count, service.MaxRetryCount)
	}

	// 超过最大重试 → 标记 failed
	if count >= service.MaxRetryCount {
		repo.MarkFailed(context.Background(), 1, "重试耗尽")
	}

	if rv.GenerationStatus != model.GenStatusFailed {
		t.Errorf("重试耗尽后状态应为 failed，got %s", rv.GenerationStatus)
	}
	if rv.ErrorMessage != "重试耗尽" {
		t.Errorf("错误信息不匹配: %s", rv.ErrorMessage)
	}
}
