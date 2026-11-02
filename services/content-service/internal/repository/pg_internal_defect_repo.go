// InternalDefectReader 的 PostgreSQL 实现：仅查脱敏列，独立于 defectColumns，
// 杜绝 root_cause / 各类配置列进入服务间流量。
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 编译期断言：PGDefectRepo 满足 InternalDefectReader。
var _ InternalDefectReader = (*PGDefectRepo)(nil)

// internalDefectColumns 内部接口专用脱敏列，顺序与 scanInternalDefect 严格对应。
const internalDefectColumns = `
	id, project_id, COALESCE(code, ''), name, COALESCE(defect_type, ''),
	COALESCE(category, ''), COALESCE(difficulty, ''), COALESCE(description, ''),
	COALESCE(target_service_id, 0),
	COALESCE(score_weight, 1.0),
	COALESCE(estimated_fix_time, 0),
	COALESCE(sort_order, 0),
	is_active`

// ListByIDs 按 ID 列表批量查询；空列表直接返回空切片不发 SQL。
// 用 = ANY($1) 单参数传数组，结果按 id 升序保证调用方稳定对齐。
func (r *PGDefectRepo) ListByIDs(ctx context.Context, ids []int64) ([]*InternalDefectView, error) {
	if len(ids) == 0 {
		return []*InternalDefectView{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+internalDefectColumns+`
		FROM content_defects
		WHERE deleted_at IS NULL AND id = ANY($1)
		ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectInternalDefects(rows)
}

// ListActiveByProject 查询项目下启用中的故障目录。
func (r *PGDefectRepo) ListActiveByProject(ctx context.Context, projectID int64) ([]*InternalDefectView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+internalDefectColumns+`
		FROM content_defects
		WHERE deleted_at IS NULL AND project_id = $1 AND is_active = true
		ORDER BY sort_order ASC, id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectInternalDefects(rows)
}

// SearchByName 按名称模糊反查故障 ID（供调用方在本地侧对齐关键词条件）。
// 仅返回 id/name 两个脱敏字段；空关键词不发 SQL，结果上限 100 防止全表扫。
func (r *PGDefectRepo) SearchByName(ctx context.Context, keyword string) ([]*InternalDefectName, error) {
	if keyword == "" {
		return []*InternalDefectName{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, name
		FROM content_defects
		WHERE deleted_at IS NULL AND name ILIKE $1
		ORDER BY id
		LIMIT 100`, "%"+keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*InternalDefectName, 0)
	for rows.Next() {
		n := &InternalDefectName{}
		if err := rows.Scan(&n.ID, &n.Name); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// collectInternalDefects 遍历结果集收集脱敏投影。
func collectInternalDefects(rows pgx.Rows) ([]*InternalDefectView, error) {
	items := make([]*InternalDefectView, 0)
	for rows.Next() {
		v := &InternalDefectView{}
		if err := rows.Scan(
			&v.ID, &v.ProjectID, &v.Code, &v.Name, &v.DefectType,
			&v.Category, &v.Difficulty, &v.Description,
			&v.TargetServiceID, &v.ScoreWeight, &v.EstimatedFixMin,
			&v.SortOrder, &v.IsActive,
		); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
