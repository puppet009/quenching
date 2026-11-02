// InternalDefectReader 的内存实现：从完整 model.Defect 复制脱敏字段，
// 供单测/开发期使用，语义与 PG 实现对齐。
package repository

import (
	"context"
	"sort"
	"strings"

	"github.com/quenching/content-service/internal/model"
)

// 编译期断言：InMemoryDefectRepo 满足 InternalDefectReader。
var _ InternalDefectReader = (*InMemoryDefectRepo)(nil)

// toInternalView 投影为脱敏视图（复制值，调用方拿到后与内存对象解耦）。
func toInternalView(d *model.Defect) InternalDefectView {
	return InternalDefectView{
		ID:              d.ID,
		ProjectID:       d.ProjectID,
		Code:            d.Code,
		Name:            d.Name,
		DefectType:      d.DefectType,
		Category:        d.Category,
		Difficulty:      d.Difficulty,
		Description:     d.Description,
		TargetServiceID: d.TargetServiceID,
		ScoreWeight:     d.ScoreWeight,
		EstimatedFixMin: int32(d.EstimatedFixTime),
		SortOrder:       int32(d.SortOrder),
		IsActive:        d.IsActive,
	}
}

// ListByIDs 按 ID 列表批量查询，结果按 ID 升序。
func (r *InMemoryDefectRepo) ListByIDs(_ context.Context, ids []int64) ([]*InternalDefectView, error) {
	if len(ids) == 0 {
		return []*InternalDefectView{}, nil
	}
	wanted := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]*InternalDefectView, 0, len(ids))
	for id := range wanted {
		if d, ok := r.items[id]; ok {
			v := toInternalView(d)
			items = append(items, &v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// ListActiveByProject 查询项目下启用中的故障目录，按 SortOrder、ID 升序。
func (r *InMemoryDefectRepo) ListActiveByProject(_ context.Context, projectID int64) ([]*InternalDefectView, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type ordered struct {
		view      InternalDefectView
		sortOrder int
	}
	entries := make([]ordered, 0)
	for _, d := range r.items {
		if d.ProjectID == projectID && d.IsActive {
			entries = append(entries, ordered{view: toInternalView(d), sortOrder: d.SortOrder})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].sortOrder != entries[j].sortOrder {
			return entries[i].sortOrder < entries[j].sortOrder
		}
		return entries[i].view.ID < entries[j].view.ID
	})

	items := make([]*InternalDefectView, 0, len(entries))
	for i := range entries {
		v := entries[i].view
		items = append(items, &v)
	}
	return items, nil
}

// SearchByName 按名称大小写不敏感包含匹配，结果按 ID 升序、上限 100，语义对齐 PG 实现。
func (r *InMemoryDefectRepo) SearchByName(_ context.Context, keyword string) ([]*InternalDefectName, error) {
	if keyword == "" {
		return []*InternalDefectName{}, nil
	}
	needle := strings.ToLower(keyword)

	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]int64, 0)
	for id := range r.items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	items := make([]*InternalDefectName, 0)
	for _, id := range ids {
		d := r.items[id]
		if strings.Contains(strings.ToLower(d.Name), needle) {
			items = append(items, &InternalDefectName{ID: d.ID, Name: d.Name})
			if len(items) >= 100 {
				break
			}
		}
	}
	return items, nil
}
