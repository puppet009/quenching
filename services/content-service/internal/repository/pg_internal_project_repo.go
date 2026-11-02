// InternalProjectReader 的 PostgreSQL 实现：只查最小投影列，
// 杜绝 deploy_config / env_config 等业务配置进入服务间流量。
package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 编译期断言：internalProjectRepo 满足 InternalProjectReader。
var _ InternalProjectReader = (*internalProjectRepo)(nil)

// internalProjectRepo 内部只读查询的组合实现：projects 与 project_services
// 同属 content 域，单一实现统一暴露三方法，装配处只注册一个端口。
type internalProjectRepo struct {
	pool *pgxpool.Pool
}

// NewInternalProjectRepo 创建内部只读查询实现。
func NewInternalProjectRepo(pool *pgxpool.Pool) InternalProjectReader {
	return &internalProjectRepo{pool: pool}
}

// ListProjectsByIDs 按 ID 批量查询项目最小投影；空列表直接返回空切片。
func (r *internalProjectRepo) ListProjectsByIDs(
	ctx context.Context, ids []int64,
) ([]*InternalProjectView, error) {
	if len(ids) == 0 {
		return []*InternalProjectView{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, COALESCE(name, '')
		FROM projects
		WHERE deleted_at IS NULL AND id = ANY($1)
		ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*InternalProjectView, 0)
	for rows.Next() {
		v := &InternalProjectView{}
		if err := rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// SearchProjectsByName 按名称模糊反查项目；空关键词不发 SQL，上限 100。
func (r *internalProjectRepo) SearchProjectsByName(
	ctx context.Context, keyword string,
) ([]*InternalProjectView, error) {
	if keyword == "" {
		return []*InternalProjectView{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, COALESCE(name, '')
		FROM projects
		WHERE deleted_at IS NULL AND name ILIKE $1
		ORDER BY id
		LIMIT 100`, "%"+keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*InternalProjectView, 0)
	for rows.Next() {
		v := &InternalProjectView{}
		if err := rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// ListServicesByIDs 按 ID 批量查询项目服务最小投影；空列表直接返回空切片。
func (r *internalProjectRepo) ListServicesByIDs(
	ctx context.Context, ids []int64,
) ([]*InternalProjectServiceView, error) {
	if len(ids) == 0 {
		return []*InternalProjectServiceView{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, COALESCE(name, ''), COALESCE(display_name, '')
		FROM project_services
		WHERE deleted_at IS NULL AND id = ANY($1)
		ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*InternalProjectServiceView, 0)
	for rows.Next() {
		v := &InternalProjectServiceView{}
		if err := rows.Scan(&v.ID, &v.Name, &v.DisplayName); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
