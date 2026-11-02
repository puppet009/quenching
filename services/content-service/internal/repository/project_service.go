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

type ProjectServiceRepo struct {
	pool *pgxpool.Pool
}

func NewProjectServiceRepo(pool *pgxpool.Pool) *ProjectServiceRepo {
	return &ProjectServiceRepo{pool: pool}
}

// Create 插入一条项目服务记录。
func (r *ProjectServiceRepo) Create(ctx context.Context, s *model.ProjectService) error {
	ports, _ := json.Marshal(s.Ports)
	env, _ := json.Marshal(s.EnvConfig)
	resDef, _ := json.Marshal(s.ResourceDefault)
	resMax, _ := json.Marshal(s.ResourceMax)
	deps, _ := json.Marshal(s.Dependencies)
	mon, _ := json.Marshal(s.MonitoringConfig)
	logCfg, _ := json.Marshal(s.LogConfig)

	return r.pool.QueryRow(ctx, `
		INSERT INTO project_services (
			project_id, name, display_name, service_type, tech_stack, image,
			ports, env_config, resource_default, resource_max,
			dependencies, monitoring_config, log_config, sort_order
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, $10::jsonb,
		          $11::jsonb, $12::jsonb, $13::jsonb, $14)
		RETURNING id, created_at, updated_at`,
		s.ProjectID, s.Name, s.DisplayName, s.ServiceType, s.TechStack, s.Image,
		string(ports), string(env), string(resDef), string(resMax),
		string(deps), string(mon), string(logCfg), s.SortOrder,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
}

// GetByID 根据ID查询单条服务。
func (r *ProjectServiceRepo) GetByID(ctx context.Context, id int64) (*model.ProjectService, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, project_id, name, display_name, service_type, tech_stack, image,
		       ports, env_config, resource_default, resource_max,
		       dependencies, monitoring_config, log_config, sort_order,
		       created_at, updated_at
		FROM project_services WHERE id = $1`, id)
	return scanProjectService(row)
}

// ListByProjectID 按项目ID查询服务列表，按 sort_order 排序。
func (r *ProjectServiceRepo) ListByProjectID(ctx context.Context, projectID int64, serviceType string) ([]model.ProjectService, error) {
	where := "WHERE project_id = $1"
	args := []interface{}{projectID}
	idx := 2
	if serviceType != "" {
		where += fmt.Sprintf(" AND service_type = $%d", idx)
		args = append(args, serviceType)
		idx++
	}

	query := fmt.Sprintf(`
		SELECT id, project_id, name, display_name, service_type, tech_stack, image,
		       ports, env_config, resource_default, resource_max,
		       dependencies, monitoring_config, log_config, sort_order,
		       created_at, updated_at
		FROM project_services %s ORDER BY sort_order ASC, id ASC`, where)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.ProjectService, 0, 8)
	for rows.Next() {
		s, err := scanProjectService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// Update 更新服务配置（动态拼接 SET 子句）。
func (r *ProjectServiceRepo) Update(ctx context.Context, id int64, fields map[string]interface{}) error {
	setClauses := []string{}
	args := []interface{}{id}
	idx := 2

	addStringField := func(key string) {
		if v, ok := fields[key]; ok {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, idx))
			args = append(args, v)
			idx++
		}
	}
	addJSONBField := func(key string) {
		if v, ok := fields[key]; ok {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d::jsonb", key, idx))
			b, _ := json.Marshal(v)
			args = append(args, string(b))
			idx++
		}
	}
	addIntField := func(key string) {
		if v, ok := fields[key]; ok {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, idx))
			args = append(args, v)
			idx++
		}
	}

	addStringField("name")
	addStringField("display_name")
	addStringField("service_type")
	addStringField("tech_stack")
	addStringField("image")
	addJSONBField("ports")
	addJSONBField("env_config")
	addJSONBField("resource_default")
	addJSONBField("resource_max")
	addJSONBField("dependencies")
	addJSONBField("monitoring_config")
	addJSONBField("log_config")
	addIntField("sort_order")

	if len(setClauses) == 0 {
		return nil
	}
	setClauses = append(setClauses, "updated_at = now()")

	query := fmt.Sprintf("UPDATE project_services SET %s WHERE id = $1",
		joinComma(setClauses))

	ct, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 删除服务。
func (r *ProjectServiceRepo) Delete(ctx context.Context, id int64) error {
	ct, err := r.pool.Exec(ctx, "DELETE FROM project_services WHERE id = $1", id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanProjectService 扫描单行 project_services。
func scanProjectService(row scannable) (*model.ProjectService, error) {
	var s model.ProjectService
	var portsJSON, envJSON, resDefJSON, resMaxJSON, depsJSON, monJSON, logJSON []byte
	err := row.Scan(&s.ID, &s.ProjectID, &s.Name, &s.DisplayName,
		&s.ServiceType, &s.TechStack, &s.Image,
		&portsJSON, &envJSON, &resDefJSON, &resMaxJSON,
		&depsJSON, &monJSON, &logJSON, &s.SortOrder,
		&s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(portsJSON, &s.Ports)
	_ = json.Unmarshal(envJSON, &s.EnvConfig)
	_ = json.Unmarshal(resDefJSON, &s.ResourceDefault)
	_ = json.Unmarshal(resMaxJSON, &s.ResourceMax)
	_ = json.Unmarshal(depsJSON, &s.Dependencies)
	_ = json.Unmarshal(monJSON, &s.MonitoringConfig)
	_ = json.Unmarshal(logJSON, &s.LogConfig)
	return &s, nil
}

func joinComma(items []string) string {
	result := ""
	for i, s := range items {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
