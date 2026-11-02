package service

import (
	"context"
	"errors"

	"github.com/quenching/content-service/internal/dto"
	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
)

type ProjectServiceService struct {
	repo *repository.ProjectServiceRepo
}

func NewProjectServiceService(repo *repository.ProjectServiceRepo) *ProjectServiceService {
	return &ProjectServiceService{repo: repo}
}

// Create 创建项目服务。
func (s *ProjectServiceService) Create(ctx context.Context, req dto.CreateProjectServiceReq) (*model.ProjectService, error) {
	svc := &model.ProjectService{
		ProjectID:   req.ProjectID,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		ServiceType: req.ServiceType,
		TechStack:   req.TechStack,
		Image:       req.Image,
		SortOrder:   req.SortOrder,
	}
	// JSONB 数组字段初始化
	if req.Ports != nil {
		svc.Ports = toJSONArray(req.Ports)
	} else {
		svc.Ports = model.JSONArray{}
	}
	if req.Dependencies != nil {
		svc.Dependencies = toJSONArray(req.Dependencies)
	} else {
		svc.Dependencies = model.JSONArray{}
	}
	// JSONB 对象字段初始化
	if req.EnvConfig != nil {
		svc.EnvConfig = req.EnvConfig
	} else {
		svc.EnvConfig = model.JSON{}
	}
	if req.ResourceDefault != nil {
		svc.ResourceDefault = req.ResourceDefault
	} else {
		svc.ResourceDefault = model.JSON{}
	}
	if req.ResourceMax != nil {
		svc.ResourceMax = req.ResourceMax
	} else {
		svc.ResourceMax = model.JSON{}
	}
	if req.MonitoringConfig != nil {
		svc.MonitoringConfig = req.MonitoringConfig
	} else {
		svc.MonitoringConfig = model.JSON{}
	}
	if req.LogConfig != nil {
		svc.LogConfig = req.LogConfig
	} else {
		svc.LogConfig = model.JSON{}
	}

	if err := s.repo.Create(ctx, svc); err != nil {
		return nil, err
	}
	return svc, nil
}

// Get 根据ID获取服务详情。
func (s *ProjectServiceService) Get(ctx context.Context, id int64) (*model.ProjectService, error) {
	svc, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrProjectServiceNotFound
	}
	return svc, err
}

// ListByProjectID 按项目ID列出服务，可选按类型过滤。
func (s *ProjectServiceService) ListByProjectID(ctx context.Context, projectID int64, serviceType string) (*dto.ProjectServiceListResp, error) {
	list, err := s.repo.ListByProjectID(ctx, projectID, serviceType)
	if err != nil {
		return nil, err
	}
	return &dto.ProjectServiceListResp{List: list}, nil
}

// Update 更新服务配置。
func (s *ProjectServiceService) Update(ctx context.Context, id int64, req dto.UpdateProjectServiceReq) error {
	fields := map[string]interface{}{}
	if req.Name != nil {
		fields["name"] = *req.Name
	}
	if req.DisplayName != nil {
		fields["display_name"] = *req.DisplayName
	}
	if req.ServiceType != nil {
		fields["service_type"] = *req.ServiceType
	}
	if req.TechStack != nil {
		fields["tech_stack"] = *req.TechStack
	}
	if req.Image != nil {
		fields["image"] = *req.Image
	}
	if req.Ports != nil {
		fields["ports"] = *req.Ports
	}
	if req.EnvConfig != nil {
		fields["env_config"] = *req.EnvConfig
	}
	if req.ResourceDefault != nil {
		fields["resource_default"] = *req.ResourceDefault
	}
	if req.ResourceMax != nil {
		fields["resource_max"] = *req.ResourceMax
	}
	if req.Dependencies != nil {
		fields["dependencies"] = *req.Dependencies
	}
	if req.MonitoringConfig != nil {
		fields["monitoring_config"] = *req.MonitoringConfig
	}
	if req.LogConfig != nil {
		fields["log_config"] = *req.LogConfig
	}
	if req.SortOrder != nil {
		fields["sort_order"] = *req.SortOrder
	}

	if err := s.repo.Update(ctx, id, fields); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrProjectServiceNotFound
		}
		return err
	}
	return nil
}

// Delete 删除服务。
func (s *ProjectServiceService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrProjectServiceNotFound
		}
		return err
	}
	return nil
}

// toJSONArray 将 interface{} 安全转为 JSONArray。
func toJSONArray(v interface{}) model.JSONArray {
	if arr, ok := v.([]interface{}); ok {
		return arr
	}
	return model.JSONArray{v}
}
