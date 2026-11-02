// 修改记录采集的 HTTP 适配器（M5）。
//
// training-service 逐次登记 write_file 修改，本服务经单端点取回：
//
//	GET /internal/v1/sessions/:id/changes
//
// 只做取数与反序列化；章节 Markdown 由 service 层规则生成。无记录返回空切片。
package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/quenching/review-service/internal/model"
)

// HTTPChangeRepo 基于服务间 HTTP 的修改记录采集仓储。
type HTTPChangeRepo struct {
	client *internalClient
}

// NewHTTPChangeRepo 创建修改记录 HTTP 仓储。
func NewHTTPChangeRepo(baseURL, token string) *HTTPChangeRepo {
	return &HTTPChangeRepo{client: newInternalClient(baseURL, token)}
}

// CollectSessionChanges 取回一次会话的全部文件修改记录。
func (r *HTTPChangeRepo) CollectSessionChanges(
	ctx context.Context, sessionID int64,
) (*model.ChangeAnalysis, error) {
	if r == nil || r.client == nil || r.client.baseURL == "" {
		return nil, errors.New("change repo: training_service.base_url is not configured")
	}
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}

	raw := []model.ChangeItem{}
	path := fmt.Sprintf("/internal/v1/sessions/%d/changes", sessionID)
	if err := r.client.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	return &model.ChangeAnalysis{TotalCount: len(raw), Changes: raw}, nil
}
