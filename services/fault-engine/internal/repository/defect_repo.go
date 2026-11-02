// Package repository 故障剧本数据访问层。
//
// 以 Repository 接口抽象存储，内存实现供单测/开发期使用；
// 生产环境替换为 GORM/PostgreSQL 实现，接口签名保持不变。
package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/quenching/fault-engine/internal/model"
)

// DefectQuery 剧本列表查询参数。
type DefectQuery struct {
	TemplateSlug string
	ProjectID    int64  // 项目ID，必填
	Keyword      string // 关键词搜索（名称/code/描述）
	Category     string // 分类筛选
	Difficulty   string // 难度筛选
	DefectType   string // 类型筛选
	IsActive     *bool  // 是否启用（nil = 全部）

	Page     int // 页码，从1开始
	PageSize int // 每页条数
}

// DefectPage 分页结果。
type DefectPage struct {
	Items    []*model.Defect `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// DefectRepository 故障剧本仓储接口。
type DefectRepository interface {
	List(context.Context, DefectQuery) (*DefectPage, error)
	GetByID(context.Context, int64) (*model.Defect, error)
	GetByCode(context.Context, int64, string) (*model.Defect, error)
}

// ——— 内存实现（供测试 / 开发期使用）———

// InMemoryDefectRepo 内存版仓储，线程安全。
type InMemoryDefectRepo struct {
	mu     sync.RWMutex
	items  map[int64]*model.Defect
	nextID int64
}

// NewInMemoryDefectRepo 创建内存仓储。
func NewInMemoryDefectRepo() *InMemoryDefectRepo {
	return &InMemoryDefectRepo{
		items:  make(map[int64]*model.Defect),
		nextID: 1,
	}
}

func (r *InMemoryDefectRepo) List(_ context.Context, q DefectQuery) (*DefectPage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []*model.Defect
	for _, d := range r.items {
		if q.ProjectID > 0 && d.ProjectID != q.ProjectID {
			continue
		}
		if q.TemplateSlug != "" {
			found := false
			for _, b := range d.TemplateBindings {
				if b.TemplateSlug == q.TemplateSlug {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if q.Category != "" && d.Category != q.Category {
			continue
		}
		if q.Difficulty != "" && d.Difficulty != q.Difficulty {
			continue
		}
		if q.DefectType != "" && d.DefectType != q.DefectType {
			continue
		}
		if q.IsActive != nil && d.IsActive != *q.IsActive {
			continue
		}
		if q.Keyword != "" {
			kw := q.Keyword
			if !containsKeyword(d.Name, kw) && !containsKeyword(d.Code, kw) && !containsKeyword(d.Description, kw) {
				continue
			}
		}
		filtered = append(filtered, d)
	}

	// 按 sort_order 升序，同序按 ID 升序
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].SortOrder != filtered[j].SortOrder {
			return filtered[i].SortOrder < filtered[j].SortOrder
		}
		return filtered[i].ID < filtered[j].ID
	})

	total := len(filtered)

	// 分页
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= total {
		return &DefectPage{Items: []*model.Defect{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	if end > total {
		end = total
	}

	return &DefectPage{
		Items:    filtered[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (r *InMemoryDefectRepo) GetByID(_ context.Context, id int64) (*model.Defect, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.items[id]
	if !ok {
		return nil, ErrDefectNotFound
	}
	return d, nil
}

func (r *InMemoryDefectRepo) GetByCode(_ context.Context, projectID int64, code string) (*model.Defect, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.items {
		if d.ProjectID == projectID && d.Code == code {
			return d, nil
		}
	}
	return nil, ErrDefectNotFound
}

func (r *InMemoryDefectRepo) Create(_ context.Context, defect *model.Defect) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查 code 唯一性
	for _, d := range r.items {
		if d.ProjectID == defect.ProjectID && d.Code == defect.Code {
			return fmt.Errorf("%w: code=%s", ErrDefectCodeDuplicate, defect.Code)
		}
	}

	now := time.Now()
	defect.ID = r.nextID
	r.nextID++
	defect.CreatedAt = now
	defect.UpdatedAt = now
	if defect.SortOrder == 0 {
		defect.SortOrder = int(defect.ID) // 默认按 ID 顺序
	}

	clone := *defect
	r.items[defect.ID] = &clone
	return nil
}

func (r *InMemoryDefectRepo) Update(_ context.Context, defect *model.Defect) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.items[defect.ID]
	if !ok {
		return ErrDefectNotFound
	}

	// 检查 code 唯一性（排除自身）
	for _, d := range r.items {
		if d.ID != defect.ID && d.ProjectID == defect.ProjectID && d.Code == defect.Code {
			return fmt.Errorf("%w: code=%s", ErrDefectCodeDuplicate, defect.Code)
		}
	}

	defect.CreatedAt = existing.CreatedAt
	defect.UpdatedAt = time.Now()

	clone := *defect
	r.items[defect.ID] = &clone
	return nil
}

func (r *InMemoryDefectRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return ErrDefectNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *InMemoryDefectRepo) SetActive(_ context.Context, id int64, active bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.items[id]
	if !ok {
		return ErrDefectNotFound
	}
	d.IsActive = active
	d.UpdatedAt = time.Now()
	return nil
}

func (r *InMemoryDefectRepo) Reorder(_ context.Context, projectID int64, id1, id2 int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	d1, ok := r.items[id1]
	if !ok || d1.ProjectID != projectID {
		return ErrDefectNotFound
	}
	d2, ok := r.items[id2]
	if !ok || d2.ProjectID != projectID {
		return ErrDefectNotFound
	}

	tmp := d1.SortOrder
	d1.SortOrder = d2.SortOrder
	d2.SortOrder = tmp
	d1.UpdatedAt = time.Now()
	d2.UpdatedAt = time.Now()
	return nil
}

func (r *InMemoryDefectRepo) CountByProject(_ context.Context, projectID int64) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, d := range r.items {
		if d.ProjectID == projectID {
			count++
		}
	}
	return count, nil
}

// ——— 辅助函数 ———

func containsKeyword(s, kw string) bool {
	if s == "" {
		return false
	}
	if kw == "" {
		return true
	}
	return len(s) >= len(kw) && (s == kw || containsSubstring(s, kw))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
