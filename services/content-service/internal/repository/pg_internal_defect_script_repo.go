// InternalDefectScriptReader 的 PostgreSQL 实现。
// 与脱敏通道分离：单独的列清单只含复盘所需的 7 列，其中 root_cause 为答案列。
package repository

import (
	"context"
)

// 编译期断言：PGDefectRepo 满足 InternalDefectScriptReader。
var _ InternalDefectScriptReader = (*PGDefectRepo)(nil)

// internalDefectScriptColumns 剧本详情列，顺序与 scan 严格对应。
const internalDefectScriptColumns = `
	id, COALESCE(code, ''), name, COALESCE(category, ''),
	COALESCE(difficulty, ''), COALESCE(description, ''),
	COALESCE(root_cause, '')`

// ListScriptsByIDs 按 ID 列表批量查询剧本详情；空列表不发 SQL，结果按 id 升序。
func (r *PGDefectRepo) ListScriptsByIDs(ctx context.Context, ids []int64) ([]*InternalDefectScriptView, error) {
	if len(ids) == 0 {
		return []*InternalDefectScriptView{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+internalDefectScriptColumns+`
		FROM content_defects
		WHERE deleted_at IS NULL AND id = ANY($1)
		ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*InternalDefectScriptView, 0, len(ids))
	for rows.Next() {
		v := &InternalDefectScriptView{}
		if err := rows.Scan(
			&v.ID, &v.Code, &v.Name, &v.Category,
			&v.Difficulty, &v.Description, &v.RootCause,
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
