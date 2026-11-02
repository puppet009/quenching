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

type ProjectDeployConfigRepo struct {
	pool *pgxpool.Pool
}

func NewProjectDeployConfigRepo(pool *pgxpool.Pool) *ProjectDeployConfigRepo {
	return &ProjectDeployConfigRepo{pool: pool}
}

// Create 插入部署配置。每个项目只有一条部署配置（project_id UNIQUE）。
func (r *ProjectDeployConfigRepo) Create(ctx context.Context, c *model.ProjectDeployConfig) error {
	helmVals, _ := json.Marshal(c.HelmValues)
	ingress, _ := json.Marshal(c.IngressConfig)
	storage, _ := json.Marshal(c.StorageConfig)
	netPol, _ := json.Marshal(c.NetworkPolicy)
	health, _ := json.Marshal(c.HealthCheck)
	access, _ := json.Marshal(c.AccessLayerConfig)

	return r.pool.QueryRow(ctx, `
		INSERT INTO project_deploy_configs (
			project_id, k8s_namespace_template, helm_chart, helm_values,
			ingress_config, storage_config, network_policy, init_script,
			health_check, access_layer_config
		) VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9::jsonb, $10::jsonb)
		RETURNING id, created_at, updated_at`,
		c.ProjectID, c.K8sNamespaceTemplate, c.HelmChart, string(helmVals),
		string(ingress), string(storage), string(netPol), c.InitScript,
		string(health), string(access),
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

// GetByProjectID 按项目ID查询部署配置。
func (r *ProjectDeployConfigRepo) GetByProjectID(ctx context.Context, projectID int64) (*model.ProjectDeployConfig, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, project_id, k8s_namespace_template, helm_chart, helm_values,
		       ingress_config, storage_config, network_policy, init_script,
		       health_check, access_layer_config, created_at, updated_at
		FROM project_deploy_configs WHERE project_id = $1`, projectID)
	return scanProjectDeployConfig(row)
}

// Upsert 存在则更新，不存在则插入。
func (r *ProjectDeployConfigRepo) Upsert(ctx context.Context, c *model.ProjectDeployConfig) error {
	helmVals, _ := json.Marshal(c.HelmValues)
	ingress, _ := json.Marshal(c.IngressConfig)
	storage, _ := json.Marshal(c.StorageConfig)
	netPol, _ := json.Marshal(c.NetworkPolicy)
	health, _ := json.Marshal(c.HealthCheck)
	access, _ := json.Marshal(c.AccessLayerConfig)

	return r.pool.QueryRow(ctx, `
		INSERT INTO project_deploy_configs (
			project_id, k8s_namespace_template, helm_chart, helm_values,
			ingress_config, storage_config, network_policy, init_script,
			health_check, access_layer_config
		) VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9::jsonb, $10::jsonb)
		ON CONFLICT (project_id) DO UPDATE SET
			k8s_namespace_template = EXCLUDED.k8s_namespace_template,
			helm_chart = EXCLUDED.helm_chart,
			helm_values = EXCLUDED.helm_values,
			ingress_config = EXCLUDED.ingress_config,
			storage_config = EXCLUDED.storage_config,
			network_policy = EXCLUDED.network_policy,
			init_script = EXCLUDED.init_script,
			health_check = EXCLUDED.health_check,
			access_layer_config = EXCLUDED.access_layer_config,
			updated_at = now()
		RETURNING id, created_at, updated_at`,
		c.ProjectID, c.K8sNamespaceTemplate, c.HelmChart, string(helmVals),
		string(ingress), string(storage), string(netPol), c.InitScript,
		string(health), string(access),
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

// Update 更新部署配置（动态字段）。
func (r *ProjectDeployConfigRepo) Update(ctx context.Context, projectID int64, fields map[string]interface{}) error {
	setClauses := []string{}
	args := []interface{}{projectID}
	idx := 2

	addJSONBField := func(key string) {
		if v, ok := fields[key]; ok {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d::jsonb", key, idx))
			b, _ := json.Marshal(v)
			args = append(args, string(b))
			idx++
		}
	}
	addStringField := func(key string) {
		if v, ok := fields[key]; ok {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, idx))
			args = append(args, v)
			idx++
		}
	}

	addStringField("k8s_namespace_template")
	addStringField("helm_chart")
	addJSONBField("helm_values")
	addJSONBField("ingress_config")
	addJSONBField("storage_config")
	addJSONBField("network_policy")
	addStringField("init_script")
	addJSONBField("health_check")
	addJSONBField("access_layer_config")

	if len(setClauses) == 0 {
		return nil
	}
	setClauses = append(setClauses, "updated_at = now()")

	query := fmt.Sprintf("UPDATE project_deploy_configs SET %s WHERE project_id = $1",
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

// Delete 按项目ID删除部署配置。
func (r *ProjectDeployConfigRepo) Delete(ctx context.Context, projectID int64) error {
	ct, err := r.pool.Exec(ctx, "DELETE FROM project_deploy_configs WHERE project_id = $1", projectID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanProjectDeployConfig 扫描单行部署配置。
func scanProjectDeployConfig(row scannable) (*model.ProjectDeployConfig, error) {
	var c model.ProjectDeployConfig
	var helmValsJSON, ingressJSON, storageJSON, netPolJSON, healthJSON, accessJSON []byte
	err := row.Scan(&c.ID, &c.ProjectID, &c.K8sNamespaceTemplate, &c.HelmChart, &helmValsJSON,
		&ingressJSON, &storageJSON, &netPolJSON, &c.InitScript,
		&healthJSON, &accessJSON, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(helmValsJSON, &c.HelmValues)
	_ = json.Unmarshal(ingressJSON, &c.IngressConfig)
	_ = json.Unmarshal(storageJSON, &c.StorageConfig)
	_ = json.Unmarshal(netPolJSON, &c.NetworkPolicy)
	_ = json.Unmarshal(healthJSON, &c.HealthCheck)
	_ = json.Unmarshal(accessJSON, &c.AccessLayerConfig)
	return &c, nil
}
