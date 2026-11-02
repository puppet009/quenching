package service

import (
	"context"
	"errors"

	"github.com/quenching/content-service/internal/dto"
	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
)

type ProjectDeployConfigService struct {
	repo *repository.ProjectDeployConfigRepo
}

func NewProjectDeployConfigService(repo *repository.ProjectDeployConfigRepo) *ProjectDeployConfigService {
	return &ProjectDeployConfigService{repo: repo}
}

// Save 保存部署配置（Upsert：存在则更新，不存在则插入）。
func (s *ProjectDeployConfigService) Save(ctx context.Context, req dto.SaveProjectDeployConfigReq) (*model.ProjectDeployConfig, error) {
	cfg := &model.ProjectDeployConfig{
		ProjectID:            req.ProjectID,
		K8sNamespaceTemplate: req.K8sNamespaceTemplate,
		HelmChart:            req.HelmChart,
		InitScript:           req.InitScript,
	}
	if req.HelmValues != nil {
		cfg.HelmValues = req.HelmValues
	} else {
		cfg.HelmValues = model.JSON{}
	}
	if req.IngressConfig != nil {
		cfg.IngressConfig = req.IngressConfig
	} else {
		cfg.IngressConfig = model.JSON{}
	}
	if req.StorageConfig != nil {
		cfg.StorageConfig = req.StorageConfig
	} else {
		cfg.StorageConfig = model.JSON{}
	}
	if req.NetworkPolicy != nil {
		cfg.NetworkPolicy = req.NetworkPolicy
	} else {
		cfg.NetworkPolicy = model.JSON{}
	}
	if req.HealthCheck != nil {
		cfg.HealthCheck = req.HealthCheck
	} else {
		cfg.HealthCheck = model.JSON{}
	}
	if req.AccessLayerConfig != nil {
		cfg.AccessLayerConfig = req.AccessLayerConfig
	} else {
		cfg.AccessLayerConfig = model.JSON{}
	}

	if err := s.repo.Upsert(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// GetByProjectID 按项目ID获取部署配置。
func (s *ProjectDeployConfigService) GetByProjectID(ctx context.Context, projectID int64) (*model.ProjectDeployConfig, error) {
	cfg, err := s.repo.GetByProjectID(ctx, projectID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrDeployConfigNotFound
	}
	return cfg, err
}

// Update 部分更新部署配置。
func (s *ProjectDeployConfigService) Update(ctx context.Context, projectID int64, req dto.UpdateProjectDeployConfigReq) error {
	fields := map[string]interface{}{}
	if req.K8sNamespaceTemplate != nil {
		fields["k8s_namespace_template"] = *req.K8sNamespaceTemplate
	}
	if req.HelmChart != nil {
		fields["helm_chart"] = *req.HelmChart
	}
	if req.HelmValues != nil {
		fields["helm_values"] = *req.HelmValues
	}
	if req.IngressConfig != nil {
		fields["ingress_config"] = *req.IngressConfig
	}
	if req.StorageConfig != nil {
		fields["storage_config"] = *req.StorageConfig
	}
	if req.NetworkPolicy != nil {
		fields["network_policy"] = *req.NetworkPolicy
	}
	if req.InitScript != nil {
		fields["init_script"] = *req.InitScript
	}
	if req.HealthCheck != nil {
		fields["health_check"] = *req.HealthCheck
	}
	if req.AccessLayerConfig != nil {
		fields["access_layer_config"] = *req.AccessLayerConfig
	}

	if err := s.repo.Update(ctx, projectID, fields); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrDeployConfigNotFound
		}
		return err
	}
	return nil
}

// Delete 按项目ID删除部署配置。
func (s *ProjectDeployConfigService) Delete(ctx context.Context, projectID int64) error {
	if err := s.repo.Delete(ctx, projectID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrDeployConfigNotFound
		}
		return err
	}
	return nil
}
