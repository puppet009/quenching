// 用户面项目故障只读仓储：经 fault-engine 内部接口取数（架构拆库第二批），
// 只取启用中的非敏感展示列；fault-engine 端按 sort_order、id 升序返回。
//
// 本类型保留原命名作为 service 层依赖端口的 PG/HTTP 适配器，编排层不动。
package repository

import (
	"context"

	"github.com/quenching/content-service/internal/dto"
)

// ProjectDefectRepo 用户面故障只读仓储（fault-engine client 适配器）。
type ProjectDefectRepo struct {
	client ProjectDefectReader
}

// NewProjectDefectRepo 创建只读仓储，client 为 fault-engine 内部只读客户端。
func NewProjectDefectRepo(client ProjectDefectReader) *ProjectDefectRepo {
	return &ProjectDefectRepo{client: client}
}

// ListActiveByProject 列出项目下启用中的故障概览。
func (r *ProjectDefectRepo) ListActiveByProject(
	ctx context.Context, projectID int64,
) ([]dto.ProjectDefectItem, error) {
	views, err := r.client.ListActiveByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	out := make([]dto.ProjectDefectItem, 0, len(views))
	for _, v := range views {
		item := dto.ProjectDefectItem{
			ID:              v.ID,
			ProjectID:       v.ProjectID,
			Code:            v.Code,
			Name:            v.Name,
			DefectType:      v.DefectType,
			Category:        v.Category,
			Difficulty:      v.Difficulty,
			Description:     v.Description,
			ScoreWeight:     v.ScoreWeight,
			EstimatedFixMin: v.EstimatedFixMin,
			SortOrder:       v.SortOrder,
		}
		// 保持原指针语义：无目标服务时前端拿到 null 而非 0。
		if v.TargetServiceID > 0 {
			sid := v.TargetServiceID
			item.TargetServiceID = &sid
		}
		out = append(out, item)
	}
	return out, nil
}

type ProjectDefectReader interface {
	ListActiveByProject(context.Context, int64) ([]*faultDefectView, error)
}
