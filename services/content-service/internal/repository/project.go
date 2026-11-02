// Package repository 数据访问层：只做读写，不含业务规则。
// 这里演示 pgx/v5 + JSONB + 分页的标准写法，其他模块照此实现。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/content-service/internal/model"
)

// ErrNotFound 数据不存在。service 层据此转成 response.ErrNotFound。
var ErrNotFound = errors.New("record not found")

type ProjectRepo struct {
	pool *pgxpool.Pool
}

func NewProjectRepo(pool *pgxpool.Pool) *ProjectRepo {
	return &ProjectRepo{pool: pool}
}

// Create 插入。deploy_config 为 JSONB，用 json.Marshal 后传给 pgx。
func (r *ProjectRepo) Create(ctx context.Context, p *model.Project) error {
	cfg, err := json.Marshal(p.DeployConfig)
	if err != nil {
		return fmt.Errorf("序列化 deploy_config: %w", err)
	}
	stack, err := json.Marshal(p.TechStack)
	if err != nil {
		return err
	}
	return r.pool.QueryRow(ctx, `
		INSERT INTO projects (name, domain, description, tech_stack, architecture_image, deploy_config, difficulty, status, sort_order)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6::jsonb, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		p.Name, p.Domain, p.Description, string(stack), p.ArchitectureImage, cfg, p.Difficulty, p.Status, p.SortOrder,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

// GetByID 查询单条。
func (r *ProjectRepo) GetByID(ctx context.Context, id int64) (*model.Project, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, domain, description, tech_stack, architecture_image, deploy_config, difficulty, status, sort_order, created_at, updated_at
		FROM projects WHERE id = $1`, id)
	return scanProject(row)
}

// List 分页 + 可选领域/状态/难度/关键词过滤。
func (r *ProjectRepo) List(ctx context.Context, domain, status, difficulty, keyword string, page, size int) ([]model.Project, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	// 生产环境请用查询构建器或固定白名单拼接，避免 SQL 注入。
	where := "WHERE 1=1"
	args := []interface{}{}
	idx := 1
	if domain != "" {
		where += fmt.Sprintf(" AND domain = $%d", idx)
		args = append(args, domain)
		idx++
	}
	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, status)
		idx++
	}
	if difficulty != "" {
		where += fmt.Sprintf(" AND difficulty = $%d", idx)
		args = append(args, difficulty)
		idx++
	}
	if keyword != "" {
		where += fmt.Sprintf(" AND (name ILIKE $%d OR description ILIKE $%d)", idx, idx)
		args = append(args, "%"+keyword+"%")
		idx++
	}

	var total int64
	countArgs := append([]interface{}{}, args...)
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM projects "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT id, name, domain, description, tech_stack, architecture_image, deploy_config, difficulty, status, sort_order, created_at, updated_at
		FROM projects %s ORDER BY sort_order ASC, id DESC LIMIT $%d OFFSET $%d`,
		where, idx, idx+1)
	args = append(args, size, (page-1)*size)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]model.Project, 0, size)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *p)
	}
	return out, total, rows.Err()
}

// Update 部分更新，支持动态字段拼接。
func (r *ProjectRepo) Update(ctx context.Context, id int64, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}

	setClauses := []string{}
	args := []interface{}{id}
	idx := 2

	// 普通字段
	plainFields := []string{"name", "domain", "description", "architecture_image", "difficulty", "status", "sort_order"}
	for _, f := range plainFields {
		if v, ok := fields[f]; ok {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", f, idx))
			args = append(args, v)
			idx++
		}
	}

	// JSONB 字段：tech_stack
	if v, ok := fields["tech_stack"]; ok {
		stackJSON, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("序列化 tech_stack: %w", err)
		}
		setClauses = append(setClauses, fmt.Sprintf("tech_stack = $%d::jsonb", idx))
		args = append(args, string(stackJSON))
		idx++
	}

	// JSONB 字段：deploy_config
	if v, ok := fields["deploy_config"]; ok {
		cfgJSON, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("序列化 deploy_config: %w", err)
		}
		setClauses = append(setClauses, fmt.Sprintf("deploy_config = $%d::jsonb", idx))
		args = append(args, string(cfgJSON))
		idx++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = now()"))
	query := fmt.Sprintf("UPDATE projects SET %s WHERE id = $1",
		joinStrings(setClauses, ", "))

	ct, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 软/硬删除按业务定，示例为硬删除。
func (r *ProjectRepo) Delete(ctx context.Context, id int64) error {
	ct, err := r.pool.Exec(ctx, "DELETE FROM projects WHERE id = $1", id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// row 抽象 pgx 行扫描，供 QueryRow / Rows 复用。
type scannable interface {
	Scan(dest ...interface{}) error
}

func scanProject(row scannable) (*model.Project, error) {
	var p model.Project
	var stackJSON, cfgJSON []byte
	err := row.Scan(&p.ID, &p.Name, &p.Domain, &p.Description, &stackJSON, &p.ArchitectureImage, &cfgJSON, &p.Difficulty, &p.Status, &p.SortOrder, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(stackJSON, &p.TechStack)
	_ = json.Unmarshal(cfgJSON, &p.DeployConfig)
	return &p, nil
}

// joinStrings 字符串拼接工具。
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, s := range parts {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
