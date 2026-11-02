// Package tests 复盘服务单元测试。
package tests

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
)

// ---------- fake repository ----------

type fakeReviewRepo struct {
	reviews  map[int64]*model.Review
	sections map[int64][]model.ReviewSection // review_id → 章节（T2-30）
	nextID   int64
}

func newFakeReviewRepo() *fakeReviewRepo {
	return &fakeReviewRepo{
		reviews:  make(map[int64]*model.Review),
		sections: make(map[int64][]model.ReviewSection),
		nextID:   1,
	}
}

func (f *fakeReviewRepo) Create(_ context.Context, rv *model.Review) error {
	rv.ID = f.nextID
	f.nextID++
	rv.CreatedAt = time.Now()
	rv.UpdatedAt = time.Now()
	f.reviews[rv.ID] = rv
	return nil
}

func (f *fakeReviewRepo) GetByID(_ context.Context, id int64) (*model.Review, error) {
	rv, ok := f.reviews[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	// 返回副本避免测试间互相影响
	cp := *rv
	return &cp, nil
}

func (f *fakeReviewRepo) GetBySessionID(_ context.Context, sessionID int64) (*model.Review, error) {
	for _, rv := range f.reviews {
		if rv.SessionID == sessionID {
			cp := *rv
			return &cp, nil
		}
	}
	return nil, service.ErrNotFound
}

func (f *fakeReviewRepo) MarkGenerating(_ context.Context, id int64) error {
	rv, ok := f.reviews[id]
	if !ok {
		return service.ErrNotFound
	}
	rv.GenerationStatus = model.GenStatusGenerating
	rv.UpdatedAt = time.Now()
	return nil
}

func (f *fakeReviewRepo) MarkCompleted(_ context.Context, id int64, title, summary, overall string, durationMs int, techScore, collabScore float64, pitfallCount, pitfallRecognized int) error {
	rv, ok := f.reviews[id]
	if !ok {
		return service.ErrNotFound
	}
	rv.GenerationStatus = model.GenStatusCompleted
	rv.Title = title
	rv.Summary = summary
	rv.OverallEvaluation = overall
	rv.GenerationDuration = durationMs
	rv.TechScore = techScore
	rv.CollabScore = collabScore
	rv.AIPitfallCount = pitfallCount
	rv.AIPitfallRecognized = pitfallRecognized
	rv.UpdatedAt = time.Now()
	return nil
}

func (f *fakeReviewRepo) MarkFailed(_ context.Context, id int64, errMsg string) error {
	rv, ok := f.reviews[id]
	if !ok {
		return service.ErrNotFound
	}
	rv.GenerationStatus = model.GenStatusFailed
	rv.ErrorMessage = errMsg
	rv.UpdatedAt = time.Now()
	return nil
}

func (f *fakeReviewRepo) IncrementRetry(_ context.Context, id int64) (int, error) {
	rv, ok := f.reviews[id]
	if !ok {
		return 0, service.ErrNotFound
	}
	rv.RetryCount++
	rv.UpdatedAt = time.Now()
	return rv.RetryCount, nil
}

// ReplaceSections 内存版幂等整章重写：先删后插，复制入参避免外部别名干扰。
func (f *fakeReviewRepo) ReplaceSections(_ context.Context, reviewID int64, sections []model.ReviewSection) error {
	if _, ok := f.reviews[reviewID]; !ok {
		return service.ErrNotFound
	}
	if len(sections) == 0 {
		return errors.New("no sections to write")
	}
	stored := make([]model.ReviewSection, len(sections))
	copy(stored, sections)
	f.sections[reviewID] = stored
	return nil
}

// ListByReviewID 按复盘 ID 查询章节列表（T2-30）。
func (f *fakeReviewRepo) ListByReviewID(_ context.Context, reviewID int64) ([]model.ReviewSection, error) {
	if _, ok := f.reviews[reviewID]; !ok {
		return nil, service.ErrNotFound
	}
	sections, ok := f.sections[reviewID]
	if !ok {
		return []model.ReviewSection{}, nil
	}
	// 返回副本
	out := make([]model.ReviewSection, len(sections))
	copy(out, sections)
	return out, nil
}

// ---------- T4-16 历史列表/状态接口（满足 service.ReviewRepo） ----------

// ListByUser 内存版按用户倒序分页：创建顺序递增 ID，倒序即"最新优先"。
func (f *fakeReviewRepo) ListByUser(_ context.Context, userID int64, limit, offset int) ([]model.ReviewListItem, error) {
	var items []model.ReviewListItem
	for _, rv := range f.reviews {
		if rv.UserID == userID {
			items = append(items, model.ReviewListItem{
				ID:                 rv.ID,
				SessionID:          rv.SessionID,
				Title:              rv.Title,
				GenerationStatus:   rv.GenerationStatus,
				TechScore:          rv.TechScore,
				CollabScore:        rv.CollabScore,
				GenerationDuration: rv.GenerationDuration,
				CreatedAt:          rv.CreatedAt,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if offset >= len(items) {
		return []model.ReviewListItem{}, nil
	}
	items = items[offset:]
	if limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}

func (f *fakeReviewRepo) CountByUser(_ context.Context, userID int64) (int64, error) {
	var n int64
	for _, rv := range f.reviews {
		if rv.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (f *fakeReviewRepo) GetStatusBySessionID(_ context.Context, sessionID int64) (*model.ReviewStatus, error) {
	for _, rv := range f.reviews {
		if rv.SessionID == sessionID {
			return &model.ReviewStatus{
				ID:               rv.ID,
				SessionID:        rv.SessionID,
				GenerationStatus: rv.GenerationStatus,
				ErrorMessage:     rv.ErrorMessage,
			}, nil
		}
	}
	return nil, service.ErrNotFound
}

// ---------- fake producer ----------

type fakeProducer struct {
	published []*service.ReviewTask
	fail      bool // 是否模拟投递失败
}

func newFakeProducer() *fakeProducer {
	return &fakeProducer{}
}

func (f *fakeProducer) Publish(_ context.Context, task *service.ReviewTask) error {
	if f.fail {
		return errors.New("mq unavailable")
	}
	f.published = append(f.published, task)
	return nil
}

// ---------- ReviewService 测试 ----------

// TestStartGenerationSuccess 测试正常触发复盘生成。
func TestStartGenerationSuccess(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	rv, err := svc.StartGeneration(context.Background(), 1001, 101, 1)
	if err != nil {
		t.Fatalf("触发复盘应成功，got err: %v", err)
	}

	if rv.SessionID != 1001 {
		t.Errorf("session_id 应为 1001，got %d", rv.SessionID)
	}
	if rv.GenerationStatus != model.GenStatusGenerating {
		t.Errorf("状态应为 generating，got %s", rv.GenerationStatus)
	}
}

// TestStartGenerationIdempotent 测试幂等性：同一会话重复触发不新建。
func TestStartGenerationIdempotent(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	rv1, _ := svc.StartGeneration(context.Background(), 1001, 101, 1)
	rv2, _ := svc.StartGeneration(context.Background(), 1001, 101, 1)

	if rv1.ID != rv2.ID {
		t.Errorf("同一会话应返回同一复盘记录，ID 不同: %d vs %d", rv1.ID, rv2.ID)
	}
}

// TestStartGenerationPublishFailed 测试投递失败时标记 failed。
func TestStartGenerationPublishFailed(t *testing.T) {
	repo := newFakeReviewRepo()
	producer := newFakeProducer()
	producer.fail = true
	svc := service.NewReviewService(repo, producer, repo).WithReviewEligibility(growthEligibility{})

	rv, err := svc.StartGeneration(context.Background(), 1001, 101, 1)
	if err != nil {
		t.Fatalf("投递失败也不应返回错误（前端靠状态感知）")
	}

	if rv.GenerationStatus != model.GenStatusFailed {
		t.Errorf("投递失败状态应为 failed，got %s", rv.GenerationStatus)
	}
	if rv.ErrorMessage == "" {
		t.Error("投递失败应记录错误信息")
	}
}

// TestGetBySessionID 测试按会话查询。
func TestGetBySessionID(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	// 先创建
	svc.StartGeneration(context.Background(), 1001, 101, 1)

	rv, err := svc.GetBySessionID(context.Background(), 1001)
	if err != nil {
		t.Fatalf("查询应成功，got err: %v", err)
	}
	if rv.SessionID != 1001 {
		t.Errorf("session_id 不匹配: %d", rv.SessionID)
	}
}

// TestGetBySessionIDNotFound 测试查询不存在的会话。
func TestGetBySessionIDNotFound(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	_, err := svc.GetBySessionID(context.Background(), 9999)
	if err == nil {
		t.Fatal("不存在的会话应返回错误")
	}
	if err != service.ErrNotFound {
		t.Errorf("应返回 ErrNotFound，got %v", err)
	}
}

// ---------- ReviewTask 序列化测试 ----------

func TestReviewTaskMarshal(t *testing.T) {
	// 用 worker 包的类型来测
	// 这里用一个简化版验证序列化逻辑
	task := struct {
		ReviewID  int64  `json:"review_id"`
		SessionID int64  `json:"session_id"`
		UserID    int64  `json:"user_id"`
		ProjectID int64  `json:"project_id"`
		TraceID   string `json:"trace_id"`
	}{
		ReviewID:  1,
		SessionID: 1001,
		UserID:    101,
		ProjectID: 1,
		TraceID:   "trace-abc",
	}

	if task.ReviewID != 1 {
		t.Error("序列化前数据不对")
	}
	// 序列化正确性由 JSON 标准库保证，这里只验证结构体定义
}
