// 复盘输入取数的 HTTP 适配器（架构拆库第五批）。
//
// 替代旧的 ReviewInputRepo PG 直查：会话难度、操作时间线、本次会话剧本（含
// 剧本预设根因）统一由 training-service 在服务端聚合后经单端点输出，本服务
// 不再跨库 JOIN training_sessions/session_operations/session_defects/defects。
//
//	GET /internal/v1/sessions/:id/review-input
//
// 领域模型 model.ReviewInput 与 generator/Prompt 零改动（JSON 标签即契约）。
// 指标快照 MVP 仍无数据源，保持空切片，待 sandbox-service 提供端口后补查。
package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/quenching/review-service/internal/model"
)

// HTTPReviewInputRepo 基于服务间 HTTP 的复盘输入仓储。
type HTTPReviewInputRepo struct {
	client *internalClient
}

// NewHTTPReviewInputRepo 创建复盘输入 HTTP 仓储。
// baseURL 为空时取数返回明确错误，由 worker 标记失败，不生成空洞复盘。
func NewHTTPReviewInputRepo(baseURL, token string) *HTTPReviewInputRepo {
	return &HTTPReviewInputRepo{client: newInternalClient(baseURL, token)}
}

// 编译期断言：满足 service.InputProvider（结构化匹配，无需 import service）。
var _ interface {
	LoadReviewInput(ctx context.Context, sessionID int64) (*model.ReviewInput, error)
} = (*HTTPReviewInputRepo)(nil)

// LoadReviewInput 从 training-service 聚合端点取一次复盘所需的全部输入。
func (r *HTTPReviewInputRepo) LoadReviewInput(
	ctx context.Context, sessionID int64,
) (*model.ReviewInput, error) {
	if r == nil || r.client == nil || r.client.baseURL == "" {
		return nil, errors.New("review input repo: training_service.base_url is not configured")
	}
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}

	in := &model.ReviewInput{
		SessionID: sessionID,
		Timeline:  []model.TimelineOperation{},
		Scripts:   []model.FaultScript{},
		Metrics:   []model.MetricSnapshot{},
	}
	path := fmt.Sprintf("/internal/v1/sessions/%d/review-input", sessionID)
	if err := r.client.do(ctx, http.MethodGet, path, nil, in); err != nil {
		return nil, err
	}

	// 保证切片非 nil（旧 PG 实现口径），Prompt 序列化稳定输出 []。
	if in.Timeline == nil {
		in.Timeline = []model.TimelineOperation{}
	}
	if in.Scripts == nil {
		in.Scripts = []model.FaultScript{}
	}
	return in, nil
}
