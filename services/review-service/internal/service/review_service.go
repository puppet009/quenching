// Package service 复盘业务服务。
package service

import (
	"context"
	"errors"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/repository"
)

// ErrNotFound 数据不存在。
var ErrNotFound = errors.New("复盘不存在")

// ReviewRepo 服务层需要的仓储接口。
type ReviewRepo interface {
	Create(ctx context.Context, rv *model.Review) error
	GetByID(ctx context.Context, id int64) (*model.Review, error)
	GetBySessionID(ctx context.Context, sessionID int64) (*model.Review, error)
	MarkFailed(ctx context.Context, id int64, errMsg string) error
	// T4-16：历史列表分页 + 轮询轻量状态。
	ListByUser(ctx context.Context, userID int64, limit, offset int) ([]model.ReviewListItem, error)
	CountByUser(ctx context.Context, userID int64) (int64, error)
	GetStatusBySessionID(ctx context.Context, sessionID int64) (*model.ReviewStatus, error)
}

// 历史分页参数约束（T4-16）：纯函数，便于单测。
const (
	historyDefaultPage = 1
	historyDefaultSize = 20
	historyMaxSize     = 100
)

// NormalizePage 钳制分页参数：page 从 1 起、size 限制在 1~100。
func NormalizePage(page, size int) (int, int) {
	if page < 1 {
		page = historyDefaultPage
	}
	if size < 1 {
		size = historyDefaultSize
	}
	if size > historyMaxSize {
		size = historyMaxSize
	}
	return page, size
}

// HistoryPage 历史列表分页结果。
type HistoryPage struct {
	Total int64                  `json:"total"`
	List  []model.ReviewListItem `json:"list"`
}

// ReviewProducer 任务生产者接口。
type ReviewProducer interface {
	Publish(ctx context.Context, task *ReviewTask) error
}

// SectionRepo 章节仓储接口。
type SectionRepo interface {
	ListByReviewID(ctx context.Context, reviewID int64) ([]model.ReviewSection, error)
}

// ReviewDetail 复盘详情（含章节明细）。
type ReviewDetail struct {
	*model.Review
	GrowthReport *rc.GrowthReport      `json:"growth_report,omitempty"`
	Sections     []model.ReviewSection `json:"sections"`
}

// ReviewService 复盘业务服务。
type ReviewService struct {
	repo        ReviewRepo
	producer    ReviewProducer
	sections    SectionRepo
	eligibility interface {
		Eligibility(context.Context, int64, int64, int64) (rc.Eligibility, error)
	}
	snapshots GrowthSnapshotStore
	progress  GrowthJobLoader
}

func (s *ReviewService) WithGrowthProgress(jobs GrowthJobLoader) *ReviewService {
	s.progress = jobs
	return s
}

func (s *ReviewService) WithReviewEligibility(e interface {
	Eligibility(context.Context, int64, int64, int64) (rc.Eligibility, error)
}) *ReviewService {
	s.eligibility = e
	return s
}
func (s *ReviewService) WithGrowthSnapshots(sources GrowthSnapshotStore) *ReviewService {
	s.snapshots = sources
	return s
}

// NewReviewService 创建复盘服务。
func NewReviewService(repo ReviewRepo, producer ReviewProducer, sections SectionRepo) *ReviewService {
	return &ReviewService{repo: repo, producer: producer, sections: sections}
}

// StartGeneration 触发复盘生成（异步）。
// 创建复盘记录并投递到消息队列，立即返回。
// 如果该会话已有复盘记录，直接返回现有记录（幂等）。
func (s *ReviewService) StartGeneration(ctx context.Context, sessionID, userID, projectID int64) (*model.Review, error) {
	if sessionID <= 0 || userID <= 0 || projectID < 0 {
		return nil, ErrNotFound
	}
	if s.eligibility == nil {
		return nil, fmt.Errorf("review eligibility is not configured")
	}
	e, eligibilityErr := s.eligibility.Eligibility(ctx, userID, sessionID, projectID)
	if eligibilityErr != nil {
		return nil, eligibilityErr
	}
	if e.UserID != userID || e.SessionID != sessionID || e.ProjectID != projectID {
		return nil, ErrNotFound
	}
	if !e.CanGenerate {
		return nil, fmt.Errorf("实训尚未结束，暂不能生成复盘")
	}
	// 先查是否已有
	existing, err := s.repo.GetBySessionID(ctx, sessionID)
	if err == nil && existing != nil {
		if existing.UserID != userID || existing.ProjectID != projectID {
			return nil, ErrNotFound
		}
		// 已有记录，如果是终态（completed/failed），不重新触发
		// 如果还在进行中，也直接返回
		return existing, nil
	}

	// 创建复盘记录
	rv := &model.Review{
		SessionID:        sessionID,
		UserID:           userID,
		ProjectID:        projectID,
		ReviewLevel:      model.ReviewLevelBasic,
		GenerationStatus: model.GenStatusPending,
	}

	var createErr error
	if s.snapshots != nil {
		frozen, freezeErr := s.snapshots.Freeze(ctx, userID, sessionID, projectID)
		if freezeErr != nil {
			return nil, freezeErr
		}
		if frozen.UserID != userID || frozen.SessionID != sessionID || frozen.ProjectID != projectID || frozen.Hash == "" {
			return nil, ErrNotFound
		}
		creator, ok := s.repo.(interface {
			CreateGrowth(context.Context, *model.Review, rc.Snapshot) error
		})
		if !ok {
			return nil, fmt.Errorf("atomic growth creation not configured")
		}
		createErr = creator.CreateGrowth(ctx, rv, frozen)
	} else {
		createErr = s.repo.Create(ctx, rv)
	}
	if createErr != nil {
		if existing, e := s.repo.GetBySessionID(ctx, sessionID); e == nil && existing.UserID == userID && existing.ProjectID == projectID {
			return existing, nil
		}
		return nil, fmt.Errorf("创建复盘记录失败: %w", createErr)
	}

	// 投递到消息队列
	task := &ReviewTask{
		ReviewID:  rv.ID,
		SessionID: sessionID,
		UserID:    userID,
		ProjectID: projectID,
	}

	if err := s.producer.Publish(ctx, task); err != nil {
		if s.snapshots != nil {
			return rv, nil
		} // Persisted due job is recovered by dispatcher.
		// 投递失败，标记 failed
		_ = s.repo.MarkFailed(ctx, rv.ID, "投递任务失败: "+err.Error())
		rv.GenerationStatus = model.GenStatusFailed
		rv.ErrorMessage = err.Error()
		return rv, nil // 不返回错误，前端可以通过状态查到失败
	}

	rv.GenerationStatus = model.GenStatusGenerating
	return rv, nil
}

// GetByID 按 ID 查询复盘。
func (s *ReviewService) GetByID(ctx context.Context, id int64) (*model.Review, error) {
	rv, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rv, nil
}

// GetBySessionID 按会话 ID 查询复盘。
func (s *ReviewService) GetBySessionID(ctx context.Context, sessionID int64) (*model.Review, error) {
	rv, err := s.repo.GetBySessionID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rv, nil
}

// GetDetail 按 ID 查询复盘详情（含章节明细）。
// 章节按 sort_order 升序排列，前端按顺序渲染。
func (s *ReviewService) GetDetail(ctx context.Context, id int64) (*ReviewDetail, error) {
	rv, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	detail := &ReviewDetail{Review: rv}
	if reader, ok := s.repo.(interface {
		GetGrowthReport(context.Context, int64) (*rc.GrowthReport, error)
	}); ok {
		growth, e := reader.GetGrowthReport(ctx, id)
		if e != nil {
			return nil, e
		}
		detail.GrowthReport = growth
	}

	// 有章节仓储时才查章节（为 nil 表示调用方未注入，返回空切片）
	if s.sections != nil {
		sections, sErr := s.sections.ListByReviewID(ctx, id)
		if sErr != nil {
			// 章节查询失败不影响主数据，打日志由上层处理（这里直接返回空）
			detail.Sections = []model.ReviewSection{}
		} else {
			detail.Sections = sections
		}
	}

	return detail, nil
}

// GetSections 按复盘 ID 查询章节列表。
func (s *ReviewService) GetSections(ctx context.Context, reviewID int64) ([]model.ReviewSection, error) {
	if s.sections == nil {
		return []model.ReviewSection{}, nil
	}
	sections, err := s.sections.ListByReviewID(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	return sections, nil
}

// GetStatus 按会话 ID 查询生成状态（T4-16 前端轮询用，轻量字段）。
func (s *ReviewService) GetStatus(ctx context.Context, sessionID int64) (*model.ReviewStatus, error) {
	st, err := s.repo.GetStatusBySessionID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if s.progress != nil {
		job, e := s.progress.Load(ctx, st.ID)
		if e == nil {
			st.Phase = job.Phase
			st.TotalSources = job.Snapshot.SourceCount
			st.ProcessedSources = job.Cursor.Ordinal - 1
			if st.ProcessedSources < 0 {
				st.ProcessedSources = 0
			}
			if st.ProcessedSources > st.TotalSources {
				st.ProcessedSources = st.TotalSources
			}
		} else if !errors.Is(e, repository.ErrNotFound) {
			return nil, e
		}
	}
	return st, nil
}

// ListHistory 分页查询当前用户的复盘历史（T4-16）。
// 明确按 uid 过滤；跨用户/越权访问的统一治理留待 T5-6。
func (s *ReviewService) ListHistory(ctx context.Context, userID int64, page, size int) (*HistoryPage, error) {
	page, size = NormalizePage(page, size)
	offset := (page - 1) * size

	total, err := s.repo.CountByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("统计复盘总数失败: %w", err)
	}
	if total == 0 {
		return &HistoryPage{Total: 0, List: []model.ReviewListItem{}}, nil
	}

	list, err := s.repo.ListByUser(ctx, userID, size, offset)
	if err != nil {
		return nil, fmt.Errorf("查询复盘历史失败: %w", err)
	}
	return &HistoryPage{Total: total, List: list}, nil
}
