// 服务间复盘协作数据服务：校验调用参数，转发到本服务属主仓储。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/quenching/review-service/internal/repository"
)

// ErrInternalReviewNotFound 复盘不存在。
var ErrInternalReviewNotFound = errors.New("复盘不存在")

// ErrInvalidReviewInput 调用方提交的协作子分参数非法。
var ErrInvalidReviewInput = errors.New("复盘协作参数无效")

// CollabScoresInput training-service 提交的一次协作评估明细。
type CollabScoresInput struct {
	SessionID              int64
	CollabScore            float64
	QuestionQualityScore   float64
	InfoUtilizationScore   float64
	JudgmentScore          float64
	ErrorRecognitionRate   float64
	EvidenceValidationRate float64
	AIPitfallCount         int
	AIPitfallRecognized    int
}

// InternalReviewService 复盘协作数据内部服务。
type InternalReviewService struct {
	reviews repository.InternalReviewStore
}

// NewInternalReviewService 创建复盘协作数据内部服务。
func NewInternalReviewService(reviews repository.InternalReviewStore) *InternalReviewService {
	return &InternalReviewService{reviews: reviews}
}

// GetCollabBySessionID 返回会话对应的复盘协作子分。
func (s *InternalReviewService) GetCollabBySessionID(
	ctx context.Context, sessionID int64,
) (*repository.InternalReviewCollabView, error) {
	if sessionID <= 0 {
		return nil, invalidReviewInput("会话ID非法")
	}
	view, err := s.reviews.GetCollabBySessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, ErrInternalReviewNotFound
	}
	return view, nil
}

// SubmitCollabScores 写入 training-service 评估的协作子分。
// 复盘行尚不存在（评分先于建记录）时不算错误，返回 updated=false。
func (s *InternalReviewService) SubmitCollabScores(
	ctx context.Context, in CollabScoresInput,
) (bool, error) {
	if in.SessionID <= 0 {
		return false, invalidReviewInput("会话ID非法")
	}
	if err := validateReviewScore(in.CollabScore, "协作总分"); err != nil {
		return false, err
	}
	if err := validateReviewScore(in.QuestionQualityScore, "提问质量分"); err != nil {
		return false, err
	}
	if err := validateReviewScore(in.InfoUtilizationScore, "信息利用率分"); err != nil {
		return false, err
	}
	if err := validateReviewScore(in.JudgmentScore, "判断分"); err != nil {
		return false, err
	}
	if err := validateReviewRate(in.ErrorRecognitionRate, "错误识别率"); err != nil {
		return false, err
	}
	if err := validateReviewRate(in.EvidenceValidationRate, "证据验证率"); err != nil {
		return false, err
	}
	if in.AIPitfallCount < 0 || in.AIPitfallRecognized < 0 {
		return false, invalidReviewInput("坑点计数不能为负")
	}
	if in.AIPitfallRecognized > in.AIPitfallCount {
		return false, invalidReviewInput("已识别坑点数不能大于坑点总数")
	}

	found, err := s.reviews.UpdateCollabScores(ctx, in.SessionID, repository.InternalReviewCollabUpdate{
		CollabScore:            in.CollabScore,
		QuestionQualityScore:   in.QuestionQualityScore,
		InfoUtilizationScore:   in.InfoUtilizationScore,
		JudgmentScore:          in.JudgmentScore,
		ErrorRecognitionRate:   in.ErrorRecognitionRate,
		EvidenceValidationRate: in.EvidenceValidationRate,
		AIPitfallCount:         in.AIPitfallCount,
		AIPitfallRecognized:    in.AIPitfallRecognized,
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// ListExistence 批量返回复盘存在性；入参去重并丢弃非法 ID。
func (s *InternalReviewService) ListExistence(
	ctx context.Context, sessionIDs []int64,
) ([]repository.InternalReviewExistence, error) {
	ids := dedupePositiveIDs(sessionIDs)
	if len(ids) == 0 {
		return nil, invalidReviewInput("会话ID集合不能为空")
	}
	return s.reviews.ListExistence(ctx, ids)
}

// validateReviewScore 校验 0-100 的有限分值。
func validateReviewScore(v float64, field string) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return invalidReviewInput(field + "必须是有限数值")
	}
	if v < 0 || v > 100 {
		return invalidReviewInput(field + "必须在 0 到 100 之间")
	}
	return nil
}

// validateReviewRate 校验 0-1 的有限比率。
func validateReviewRate(v float64, field string) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return invalidReviewInput(field + "必须是有限数值")
	}
	if v < 0 || v > 1 {
		return invalidReviewInput(field + "必须在 0 到 1 之间")
	}
	return nil
}

// invalidReviewInput 构造参数类错误，供处理器映射为 400。
func invalidReviewInput(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidReviewInput, msg)
}

// dedupePositiveIDs 去重并过滤非正 ID，保持首次出现顺序。
func dedupePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
