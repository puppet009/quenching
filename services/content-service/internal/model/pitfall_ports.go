package model

import (
	"context"
	"errors"
)

// ErrPitfallNotFound 坑点配置不存在。
// 仓储层（repository.ErrNotFound）是它的别名，service/repository 两侧
// errors.Is 均可识别；归位到 service 是为了消除 service→repository 的反向依赖环。
var ErrPitfallNotFound = errors.New("pitfall config not found")

// ErrPitfallInvalid 配置校验失败。
var ErrPitfallInvalid = errors.New("pitfall config invalid")

// PitfallQuery 坑点列表查询参数（T3-18，归位编排层避免 service→repository 反向依赖）。
type PitfallQuery struct {
	DefectID    int64  // 按缺陷筛选，0 表示全部
	PitfallType string // 按类型筛选，空表示全部
	Status      string // 按状态筛选，空表示全部
	Page        int    // 页码，从 1 开始
	PageSize    int    // 每页条数
}

// PitfallPage 分页结果。Items 为解析后的强类型 Config（snake_case + 结构化三块配置），
// 与 Get/Create/Update 返回形态一致，前端列表可直接消费（T3-18 列表 JSON 契约）。
type PitfallPage struct {
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Items    []*PitfallConfig `json:"items"`
}

// PitfallAdminRepo 坑点配置后台管理仓储端口。
type PitfallAdminRepo interface {
	GetByID(ctx context.Context, id int64) (*PitfallStoredRow, error)
	List(ctx context.Context, q PitfallQuery) (*StoredPage, error)
	Create(ctx context.Context, cfg *PitfallConfig, createdBy int64) (*PitfallStoredRow, error)
	Update(ctx context.Context, id int64, cfg *PitfallConfig) error
	UpdateStatus(ctx context.Context, id int64, status string) error
	Delete(ctx context.Context, id int64) error
}

// StoredPage 仓储层分页结果：保留 StoredRow 原始行（JSONB 为字节），
// 由 service.List 统一解析为对外的 PitfallPage（Config）。
type StoredPage struct {
	Total    int64
	Page     int
	PageSize int
	Items    []PitfallStoredRow
}
