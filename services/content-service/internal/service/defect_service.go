// Package service 故障剧本管理服务。
//
// 提供剧本 CRUD、启用禁用、排序调整等管理能力。
// 配置校验在服务层完成，确保写入数据库的剧本配置可执行。
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
)

// DefectService 故障剧本管理服务。
type DefectService struct {
	repo              repository.DefectRepository
	resolveTemplate   func(context.Context, string) ([]string, error)
	validateExecution func(context.Context, json.RawMessage, bool) error
}

// NewDefectService 创建剧本管理服务。
func NewDefectService(repo repository.DefectRepository) *DefectService {
	return &DefectService{repo: repo}
}

// CreateDefectReq 创建剧本请求。
type CreateDefectReq struct {
	Provenance          model.JSONMap           `json:"provenance"`
	TemplateBindings    []model.TemplateBinding `json:"template_bindings"`
	IsActive            *bool                   `json:"is_active"`
	ProjectID           int64                   `json:"project_id"`
	Code                string                  `json:"code"`
	Name                string                  `json:"name"`
	DefectType          string                  `json:"defect_type"`
	Category            string                  `json:"category"`
	Difficulty          string                  `json:"difficulty"`
	Description         string                  `json:"description"`
	RootCause           string                  `json:"root_cause"`
	TargetServiceID     int64                   `json:"target_service_id"`
	TriggerConfig       model.JSONMap           `json:"trigger_config"`
	ManifestationConfig model.JSONMap           `json:"manifestation_config"`
	FixConfig           model.JSONMap           `json:"fix_config"`
	AmplifierConfig     model.JSONMap           `json:"amplifier_config"`
	FreezeRules         model.JSONMap           `json:"freeze_rules"`
	ScoreWeight         float64                 `json:"score_weight"`
	EstimatedFixTime    int                     `json:"estimated_fix_time"`
	SortOrder           int                     `json:"sort_order"`
}

// UpdateDefectReq 更新剧本请求。
type UpdateDefectReq struct {
	Provenance          *model.JSONMap           `json:"provenance"`
	TemplateBindings    *[]model.TemplateBinding `json:"template_bindings"`
	SortOrder           *int                     `json:"sort_order"`
	Name                *string                  `json:"name,omitempty"`
	DefectType          *string                  `json:"defect_type,omitempty"`
	Category            *string                  `json:"category,omitempty"`
	Difficulty          *string                  `json:"difficulty,omitempty"`
	Description         *string                  `json:"description,omitempty"`
	RootCause           *string                  `json:"root_cause,omitempty"`
	TargetServiceID     *int64                   `json:"target_service_id,omitempty"`
	TriggerConfig       *model.JSONMap           `json:"trigger_config,omitempty"`
	ManifestationConfig *model.JSONMap           `json:"manifestation_config,omitempty"`
	FixConfig           *model.JSONMap           `json:"fix_config,omitempty"`
	AmplifierConfig     *model.JSONMap           `json:"amplifier_config,omitempty"`
	FreezeRules         *model.JSONMap           `json:"freeze_rules,omitempty"`
	ScoreWeight         *float64                 `json:"score_weight,omitempty"`
	EstimatedFixTime    *int                     `json:"estimated_fix_time,omitempty"`
}

// ListDefects 分页查询剧本列表。
func (s *DefectService) ListDefects(ctx context.Context, q repository.DefectQuery) (*repository.DefectPage, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 10
	}
	return s.repo.List(ctx, q)
}

// GetDefect 获取剧本详情。
func (s *DefectService) GetDefect(ctx context.Context, id int64) (*model.Defect, error) {
	if id <= 0 {
		return nil, fmt.Errorf("剧本ID非法")
	}
	return s.repo.GetByID(ctx, id)
}

// CreateDefect 创建剧本。
func (s *DefectService) CreateDefect(ctx context.Context, req *CreateDefectReq) (*model.Defect, error) {
	// 基础校验
	if req.ProjectID <= 0 && len(req.TemplateBindings) == 0 {
		return nil, fmt.Errorf("项目ID不能为空")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("剧本编码不能为空")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("剧本名称不能为空")
	}
	if req.DefectType == "" {
		req.DefectType = "code"
	}
	if req.Difficulty == "" {
		req.Difficulty = "easy"
	}

	// 校验 amplifier_config（即剧本的可执行配置）
	if req.AmplifierConfig != nil && len(req.AmplifierConfig) > 0 {
		if err := s.validateAmplifierConfig(ctx, req.AmplifierConfig); err != nil {
			return nil, fmt.Errorf("放大器配置校验失败: %w", err)
		}
	}

	defect := &model.Defect{
		Provenance:          defaultJSONMap(req.Provenance),
		TemplateBindings:    req.TemplateBindings,
		ProjectID:           req.ProjectID,
		Code:                req.Code,
		Name:                req.Name,
		DefectType:          req.DefectType,
		Category:            req.Category,
		Difficulty:          req.Difficulty,
		Description:         req.Description,
		RootCause:           req.RootCause,
		TargetServiceID:     req.TargetServiceID,
		TriggerConfig:       defaultJSONMap(req.TriggerConfig),
		ManifestationConfig: defaultJSONMap(req.ManifestationConfig),
		FixConfig:           defaultJSONMap(req.FixConfig),
		AmplifierConfig:     defaultJSONMap(req.AmplifierConfig),
		FreezeRules:         defaultJSONMap(req.FreezeRules),
		ScoreWeight:         req.ScoreWeight,
		EstimatedFixTime:    req.EstimatedFixTime,
		IsActive:            true, // 默认启用
		SortOrder:           req.SortOrder,
	}

	if defect.ScoreWeight == 0 {
		defect.ScoreWeight = 1.0
	}
	if len(defect.TemplateBindings) > 0 {
		defect.IsActive = false
	}
	if req.IsActive != nil {
		defect.IsActive = *req.IsActive
	}
	if err := s.validateBindings(ctx, defect); err != nil {
		return nil, err
	}
	if err := validateFields(defect); err != nil {
		return nil, err
	}

	if err := s.repo.Create(ctx, defect); err != nil {
		return nil, err
	}
	return defect, nil
}

// UpdateDefect 更新剧本（部分更新）。
func (s *DefectService) UpdateDefect(ctx context.Context, id int64, req *UpdateDefectReq) (*model.Defect, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// Work on a copy, so failed validation cannot mutate an in-memory repository.
	copyDefect := *existing
	existing = &copyDefect
	if req.Provenance != nil {
		existing.Provenance = *req.Provenance
	}
	if req.TemplateBindings != nil {
		existing.TemplateBindings = *req.TemplateBindings
	}
	if req.SortOrder != nil {
		existing.SortOrder = *req.SortOrder
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.DefectType != nil {
		existing.DefectType = *req.DefectType
	}
	if req.Category != nil {
		existing.Category = *req.Category
	}
	if req.Difficulty != nil {
		existing.Difficulty = *req.Difficulty
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.RootCause != nil {
		existing.RootCause = *req.RootCause
	}
	if req.TargetServiceID != nil {
		existing.TargetServiceID = *req.TargetServiceID
	}
	if req.TriggerConfig != nil {
		existing.TriggerConfig = *req.TriggerConfig
	}
	if req.ManifestationConfig != nil {
		existing.ManifestationConfig = *req.ManifestationConfig
	}
	if req.FixConfig != nil {
		existing.FixConfig = *req.FixConfig
	}
	if req.AmplifierConfig != nil {
		// 校验放大器配置
		if err := s.validateAmplifierConfig(ctx, *req.AmplifierConfig); err != nil {
			return nil, fmt.Errorf("放大器配置校验失败: %w", err)
		}
		existing.AmplifierConfig = *req.AmplifierConfig
	}
	if req.FreezeRules != nil {
		existing.FreezeRules = *req.FreezeRules
	}
	if req.ScoreWeight != nil {
		existing.ScoreWeight = *req.ScoreWeight
	}
	if req.EstimatedFixTime != nil {
		existing.EstimatedFixTime = *req.EstimatedFixTime
	}

	if len(existing.TemplateBindings) > 0 {
		// Any edit invalidates the acceptance of the previous configuration.
		existing.IsActive = false
		bindings := append([]model.TemplateBinding(nil), existing.TemplateBindings...)
		for i := range bindings {
			bindings[i].Status = "pending_validation"
			bindings[i].Evidence = model.JSONMap{}
		}
		existing.TemplateBindings = bindings
	}
	if err := s.validateBindings(ctx, existing); err != nil {
		return nil, err
	}
	if err := validateFields(existing); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// DeleteDefect 删除剧本。
func (s *DefectService) DeleteDefect(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("剧本ID非法")
	}
	return s.repo.Delete(ctx, id)
}

// SetActive 设置启用/禁用状态。
func (s *DefectService) SetActive(ctx context.Context, id int64, active bool) error {
	if id <= 0 {
		return fmt.Errorf("剧本ID非法")
	}
	if active {
		d, err := s.repo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if err := validateFields(d); err != nil {
			return err
		}
		if len(d.TemplateBindings) > 0 {
			copyDefect := *d
			copyDefect.IsActive = true
			if err := s.validateBindings(ctx, &copyDefect); err != nil {
				return err
			}
		}
	}
	return s.repo.SetActive(ctx, id, active)
}

// Reorder 交换两个剧本的排序。
func (s *DefectService) Reorder(ctx context.Context, projectID int64, id1, id2 int64) error {
	if projectID <= 0 || id1 <= 0 || id2 <= 0 {
		return fmt.Errorf("参数非法")
	}
	if id1 == id2 {
		return nil
	}
	return s.repo.Reorder(ctx, projectID, id1, id2)
}

// ValidateDefectConfig 校验剧本配置是否合法（可执行）。
// 返回校验结果和错误列表。
func (s *DefectService) ValidateDefectConfig(ctx context.Context, id int64) (bool, []string, error) {
	defect, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return false, nil, err
	}

	var errs []string

	// 校验 amplifier_config（ChaosBlade 配置）
	if defect.AmplifierConfig == nil || len(defect.AmplifierConfig) == 0 {
		errs = append(errs, "缺少放大器配置（amplifier_config）")
	} else {
		if err := s.validateAmplifierConfig(ctx, defect.AmplifierConfig); err != nil {
			errs = append(errs, err.Error())
		}
	}

	// 校验触发配置
	if defect.TriggerConfig == nil || len(defect.TriggerConfig) == 0 {
		errs = append(errs, "缺少触发配置（trigger_config）")
	}

	// 校验表现配置
	if defect.ManifestationConfig == nil || len(defect.ManifestationConfig) == 0 {
		errs = append(errs, "缺少表现配置（manifestation_config）")
	}

	// 校验修复配置
	if defect.FixConfig == nil || len(defect.FixConfig) == 0 {
		errs = append(errs, "缺少修复验证配置（fix_config）")
	}

	// 基础字段
	if defect.Code == "" {
		errs = append(errs, "缺少剧本编码")
	}
	if defect.Name == "" {
		errs = append(errs, "缺少剧本名称")
	}

	return len(errs) == 0, errs, nil
}

// ——— 内部工具函数 ———

// validateAmplifierConfig 校验放大器配置（即 ChaosBlade 剧本配置）。
// 利用 ParseScenario 做合法性校验。
func (s *DefectService) validateAmplifierConfig(ctx context.Context, cfg model.JSONMap) error {
	// map -> JSON -> 解析校验
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("配置序列化失败: %w", err)
	}
	if s.validateExecution == nil {
		return fmt.Errorf("执行器校验未配置")
	}
	return s.validateExecution(ctx, b, false)
}

func defaultJSONMap(m model.JSONMap) model.JSONMap {
	if m == nil {
		return model.JSONMap{}
	}
	return m
}

func (s *DefectService) SetExecutionValidator(v func(context.Context, json.RawMessage, bool) error) {
	s.validateExecution = v
}
