// 用户面项目故障只读领域服务。
package service

import (
	"context"

	"github.com/quenching/content-service/internal/dto"
	"github.com/quenching/content-service/internal/repository"
)

// ProjectDefectService 用户面故障只读服务。
type ProjectDefectService struct {
	defects *repository.ProjectDefectRepo
}

// NewProjectDefectService 创建只读服务。
func NewProjectDefectService(defects *repository.ProjectDefectRepo) *ProjectDefectService {
	return &ProjectDefectService{defects: defects}
}

// ListByProject 列出项目下启用中的故障概览。
func (s *ProjectDefectService) ListByProject(ctx context.Context, projectID int64) ([]dto.ProjectDefectItem, error) {
	return s.defects.ListActiveByProject(ctx, projectID)
}
