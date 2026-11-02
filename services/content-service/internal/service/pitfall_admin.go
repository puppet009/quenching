package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/quenching/content-service/internal/model"
)

type PitfallQuery = model.PitfallQuery
type PitfallPage = model.PitfallPage
type PitfallAdminRepo = model.PitfallAdminRepo
type StoredPage = model.StoredPage

var ErrPitfallNotFound = model.ErrPitfallNotFound
var ErrPitfallInvalid = model.ErrPitfallInvalid

// PitfallAdminService 坑点配置后台管理服务（T3-18）。
//
// 职责：
//   - 配置的 CRUD + 分页查询
//   - 配置校验（复用 model.PitfallConfig.Validate）
//   - 状态流转校验（启用前强制校验完整性）
//   - 软删除
type PitfallAdminService struct {
	repo PitfallAdminRepo
}

// NewPitfallAdminService 创建坑点配置管理服务。
func NewPitfallAdminService(repo PitfallAdminRepo) *PitfallAdminService {
	return &PitfallAdminService{repo: repo}
}

// List 分页查询坑点配置。
// 仓储返回原始行（JSONB 为字节），这里逐行解析为强类型 Config，保证列表与详情
// 返回同一套 snake_case + 结构化 JSON 契约；任一行损坏则整页报错（避免前端拿到半成品）。
func (s *PitfallAdminService) List(ctx context.Context, q PitfallQuery) (*PitfallPage, error) {
	stored, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, err
	}

	items := make([]*model.PitfallConfig, 0, len(stored.Items))
	for i := range stored.Items {
		cfg, err := model.ParsePitfallStoredRow(&stored.Items[i])
		if err != nil {
			return nil, fmt.Errorf("%w: 列表第 %d 行解析失败: %v", ErrPitfallInvalid, i, err)
		}
		items = append(items, cfg)
	}

	return &PitfallPage{
		Total:    stored.Total,
		Page:     stored.Page,
		PageSize: stored.PageSize,
		Items:    items,
	}, nil
}

// Get 按 ID 查询坑点配置详情（解析后的完整 Config）。
func (s *PitfallAdminService) Get(ctx context.Context, id int64) (*model.PitfallConfig, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrPitfallNotFound) {
			return nil, ErrPitfallNotFound
		}
		return nil, err
	}

	cfg, err := model.ParsePitfallStoredRow(row)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPitfallInvalid, err)
	}
	return cfg, nil
}

// Create 创建坑点配置。
// 先做完整校验（跳过 ID 检查，因为新建时还没有 ID），校验通过后落库。
func (s *PitfallAdminService) Create(ctx context.Context, cfg *model.PitfallConfig, createdBy int64) (*model.PitfallConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is nil", ErrPitfallInvalid)
	}
	if cfg.DefectID <= 0 {
		return nil, fmt.Errorf("%w: 缺少 defect_id", ErrPitfallInvalid)
	}

	// 新建时 ID 为 0，校验时用占位值避免 ID 检查报错
	originalID := cfg.ID
	cfg.ID = 1 // 占位值
	if err := cfg.Validate(); err != nil {
		cfg.ID = originalID
		return nil, fmt.Errorf("%w: %v", ErrPitfallInvalid, err)
	}
	cfg.ID = originalID

	// 新建默认草稿状态
	if cfg.Status == "" {
		cfg.Status = model.StatusDraft
	}

	row, err := s.repo.Create(ctx, cfg, createdBy)
	if err != nil {
		return nil, err
	}

	return model.ParsePitfallStoredRow(row)
}

// Update 更新坑点配置。
// 全量字段更新，更新前做完整校验。
func (s *PitfallAdminService) Update(ctx context.Context, id int64, cfg *model.PitfallConfig) (*model.PitfallConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is nil", ErrPitfallInvalid)
	}

	// 确认存在
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrPitfallNotFound) {
			return nil, ErrPitfallNotFound
		}
		return nil, err
	}

	cfg.ID = id
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPitfallInvalid, err)
	}

	if err := s.repo.Update(ctx, id, cfg); err != nil {
		if errors.Is(err, ErrPitfallNotFound) {
			return nil, ErrPitfallNotFound
		}
		return nil, err
	}

	return s.Get(ctx, id)
}

// UpdateStatus 更新配置状态（启用/禁用/草稿）。
// 启用前强制校验配置完整性，避免不完整配置被注入训练。
func (s *PitfallAdminService) UpdateStatus(ctx context.Context, id int64, status string) error {
	if !model.IsValidStatus(status) {
		return fmt.Errorf("%w: 非法状态 %q", ErrPitfallInvalid, status)
	}

	// 确认存在
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrPitfallNotFound) {
			return ErrPitfallNotFound
		}
		return err
	}

	// 启用前校验配置完整性
	if status == model.StatusActive {
		cfg, err := s.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("配置校验不通过，无法启用: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("配置校验不通过，无法启用: %w", err)
		}
	}

	if err := s.repo.UpdateStatus(ctx, id, status); err != nil {
		if errors.Is(err, ErrPitfallNotFound) {
			return ErrPitfallNotFound
		}
		return err
	}
	return nil
}

// Delete 软删除坑点配置。
func (s *PitfallAdminService) Delete(ctx context.Context, id int64) error {
	err := s.repo.Delete(ctx, id)
	if err != nil {
		if errors.Is(err, ErrPitfallNotFound) {
			return ErrPitfallNotFound
		}
		return err
	}
	return nil
}
