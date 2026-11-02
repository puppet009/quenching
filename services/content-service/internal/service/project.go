// Package service 业务逻辑层：核心规则与编排，调用 repository，不接触 HTTP。
package service

import (
	"context"
	"errors"

	"github.com/quenching/content-service/internal/dto"
	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
)

type ProjectService struct {
	repo *repository.ProjectRepo
}

func NewProjectService(repo *repository.ProjectRepo) *ProjectService {
	return &ProjectService{repo: repo}
}

// Create 业务规则示例：下线/草稿状态需显式传入，默认 online。
func (s *ProjectService) Create(ctx context.Context, req dto.CreateProjectReq) (*model.Project, error) {
	p := &model.Project{
		Name:              req.Name,
		Domain:            req.Domain,
		Description:       req.Description,
		TechStack:         req.TechStack,
		ArchitectureImage: req.ArchitectureImage,
		DeployConfig:      req.DeployConfig,
		Difficulty:        req.Difficulty,
		Status:            model.StatusOnline,
		SortOrder:         req.SortOrder,
	}
	if p.DeployConfig == nil {
		p.DeployConfig = model.JSON{}
	}
	if p.Difficulty == "" {
		p.Difficulty = "medium"
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *ProjectService) Get(ctx context.Context, id int64) (*model.Project, error) {
	p, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrProjectNotFound
	}
	return p, err
}

func (s *ProjectService) List(ctx context.Context, domain, status, difficulty, keyword string, page, size int) (*dto.ProjectListResp, error) {
	list, total, err := s.repo.List(ctx, domain, status, difficulty, keyword, page, size)
	if err != nil {
		return nil, err
	}
	return &dto.ProjectListResp{Total: total, List: list}, nil
}

func (s *ProjectService) Update(ctx context.Context, id int64, req dto.UpdateProjectReq) error {
	fields := map[string]interface{}{}
	if req.Name != nil {
		fields["name"] = *req.Name
	}
	if req.Domain != nil {
		fields["domain"] = *req.Domain
	}
	if req.Description != nil {
		fields["description"] = *req.Description
	}
	if req.TechStack != nil {
		fields["tech_stack"] = *req.TechStack
	}
	if req.ArchitectureImage != nil {
		fields["architecture_image"] = *req.ArchitectureImage
	}
	if req.DeployConfig != nil {
		fields["deploy_config"] = *req.DeployConfig
	}
	if req.Difficulty != nil {
		fields["difficulty"] = *req.Difficulty
	}
	if req.Status != nil {
		fields["status"] = *req.Status
	}
	if req.SortOrder != nil {
		fields["sort_order"] = *req.SortOrder
	}

	if err := s.repo.Update(ctx, id, fields); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrProjectNotFound
		}
		return err
	}
	return nil
}

func (s *ProjectService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrProjectNotFound
		}
		return err
	}
	return nil
}
