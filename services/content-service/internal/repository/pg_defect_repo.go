// PostgreSQL 版故障剧本仓储。
//
// 内存实现（defect_repo.go）供单测/开发期使用；生产与本地进程级联调走本实现，
// 以读取 migrations 000002-000004 预置的 easy/medium/advanced 故障剧本。
// 写法对齐 content-service/internal/repository/project.go（pgx/v5 + JSONB）。
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

// PGDefectRepo content_defects 表的 PG 实现。
type PGDefectRepo struct {
	pool *pgxpool.Pool
}

// 编译期断言：PGDefectRepo 满足 DefectRepository。
var _ DefectRepository = (*PGDefectRepo)(nil)

// NewPGDefectRepo 创建 PG 仓储。
func NewPGDefectRepo(pool *pgxpool.Pool) *PGDefectRepo {
	return &PGDefectRepo{pool: pool}
}

// defectColumns 查询列顺序，scanDefect 与之严格对应。
const defectColumns = `
	id, project_id, code, name, defect_type, COALESCE(category, ''),
	difficulty, COALESCE(description, ''), COALESCE(root_cause, ''),
	COALESCE(target_service_id, 0),
	trigger_config, manifestation_config, fix_config,
	COALESCE(amplifier_config, '{}'::jsonb), COALESCE(freeze_rules, '{}'::jsonb),
	score_weight::float8, COALESCE(estimated_fix_time, 0),
	is_active, sort_order, created_at, updated_at, provenance, template_bindings`

// Create 插入剧本。code 唯一冲突映射为 ErrDefectCodeDuplicate。
func (r *PGDefectRepo) Create(ctx context.Context, d *model.Defect) error {
	trigger, err := jsonMarshalMap(d.TriggerConfig)
	if err != nil {
		return err
	}
	manifestation, err := jsonMarshalMap(d.ManifestationConfig)
	if err != nil {
		return err
	}
	fix, err := jsonMarshalMap(d.FixConfig)
	if err != nil {
		return err
	}
	amplifier, err := jsonMarshalNullableMap(d.AmplifierConfig)
	if err != nil {
		return err
	}
	freeze, err := jsonMarshalNullableMap(d.FreezeRules)
	if err != nil {
		return err
	}
	provenance, err := jsonMarshalMap(d.Provenance)
	if err != nil {
		return err
	}
	bindings, err := json.Marshal(d.TemplateBindings)
	if err != nil {
		return err
	}
	if d.TemplateBindings == nil {
		bindings = []byte("[]")
	}

	err = r.pool.QueryRow(ctx, `
		INSERT INTO content_defects (
			project_id, code, name, defect_type, category, difficulty,
			description, root_cause, target_service_id,
			trigger_config, manifestation_config, fix_config,
			amplifier_config, freeze_rules,
			score_weight, estimated_fix_time, is_active, sort_order, provenance, template_bindings
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12::jsonb,
		        $13::jsonb,$14::jsonb,$15,$16,$17,$18,$19::jsonb,$20::jsonb)
		RETURNING id, created_at, updated_at`,
		d.ProjectID, d.Code, d.Name, d.DefectType, nullableString(d.Category), d.Difficulty,
		nullableString(d.Description), nullableString(d.RootCause), nullableInt64(d.TargetServiceID),
		trigger, manifestation, fix, amplifier, freeze,
		d.ScoreWeight, nullableInt(d.EstimatedFixTime), d.IsActive, d.SortOrder,
		provenance, bindings,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: code=%s", ErrDefectCodeDuplicate, d.Code)
		}
		return err
	}
	return nil
}

// GetByID 按主键查询。
func (r *PGDefectRepo) GetByID(ctx context.Context, id int64) (*model.Defect, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT "+defectColumns+" FROM content_defects WHERE id = $1 AND deleted_at IS NULL", id)
	return scanDefect(row)
}

// GetByCode 按项目+code 查询。
func (r *PGDefectRepo) GetByCode(ctx context.Context, projectID int64, code string) (*model.Defect, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT "+defectColumns+" FROM content_defects WHERE project_id = $1 AND code = $2 AND deleted_at IS NULL",
		projectID, code)
	return scanDefect(row)
}

// List 分页+条件查询，动态拼接白名单列，参数化防注入。
func (r *PGDefectRepo) List(ctx context.Context, q DefectQuery) (*DefectPage, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	where := "WHERE deleted_at IS NULL"
	args := []interface{}{}
	idx := 1
	if q.ProjectID > 0 {
		where += fmt.Sprintf(" AND project_id=$%d", idx)
		args = append(args, q.ProjectID)
		idx++
	}
	if q.TemplateSlug != "" {
		binding, _ := json.Marshal([]map[string]string{{"template_slug": q.TemplateSlug}})
		where += fmt.Sprintf(" AND template_bindings @> $%d::jsonb", idx)
		args = append(args, string(binding))
		idx++
	}
	if q.Keyword != "" {
		where += fmt.Sprintf(" AND (name ILIKE $%d OR code ILIKE $%d OR description ILIKE $%d)", idx, idx, idx)
		args = append(args, "%"+q.Keyword+"%")
		idx++
	}
	if q.Category != "" {
		where += fmt.Sprintf(" AND category = $%d", idx)
		args = append(args, q.Category)
		idx++
	}
	if q.Difficulty != "" {
		where += fmt.Sprintf(" AND difficulty = $%d", idx)
		args = append(args, q.Difficulty)
		idx++
	}
	if q.DefectType != "" {
		where += fmt.Sprintf(" AND defect_type = $%d", idx)
		args = append(args, q.DefectType)
		idx++
	}
	if q.IsActive != nil {
		where += fmt.Sprintf(" AND is_active = $%d", idx)
		args = append(args, *q.IsActive)
		idx++
	}

	var total int
	countArgs := append([]interface{}{}, args...)
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM content_defects "+where, countArgs...).Scan(&total); err != nil {
		return nil, err
	}

	query := "SELECT " + defectColumns + " FROM content_defects " + where +
		fmt.Sprintf(" ORDER BY sort_order ASC, id ASC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*model.Defect, 0, pageSize)
	for rows.Next() {
		d, err := scanDefect(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &DefectPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// Update 全字段更新（code 不可改，与内存实现一致）。
func (r *PGDefectRepo) Update(ctx context.Context, d *model.Defect) error {
	provenance, err := jsonMarshalMap(d.Provenance)
	if err != nil {
		return err
	}
	bindings, err := json.Marshal(d.TemplateBindings)
	if err != nil {
		return err
	}
	if d.TemplateBindings == nil {
		bindings = []byte("[]")
	}
	trigger, err := jsonMarshalMap(d.TriggerConfig)
	if err != nil {
		return err
	}
	manifestation, err := jsonMarshalMap(d.ManifestationConfig)
	if err != nil {
		return err
	}
	fix, err := jsonMarshalMap(d.FixConfig)
	if err != nil {
		return err
	}
	amplifier, err := jsonMarshalNullableMap(d.AmplifierConfig)
	if err != nil {
		return err
	}
	freeze, err := jsonMarshalNullableMap(d.FreezeRules)
	if err != nil {
		return err
	}

	ct, err := r.pool.Exec(ctx, `
		UPDATE content_defects SET
			name = $2, defect_type = $3, category = $4, difficulty = $5,
			description = $6, root_cause = $7, target_service_id = $8,
			trigger_config = $9::jsonb, manifestation_config = $10::jsonb,
			fix_config = $11::jsonb, amplifier_config = $12::jsonb,
			freeze_rules = $13::jsonb, score_weight = $14,
			estimated_fix_time = $15, updated_at = now(), provenance=$16::jsonb,
			template_bindings=$17::jsonb, is_active=$18, sort_order=$19
		WHERE id = $1 AND deleted_at IS NULL`,
		d.ID, d.Name, d.DefectType, nullableString(d.Category), d.Difficulty,
		nullableString(d.Description), nullableString(d.RootCause), nullableInt64(d.TargetServiceID),
		trigger, manifestation, fix, amplifier, freeze,
		d.ScoreWeight, nullableInt(d.EstimatedFixTime),
		provenance, bindings, d.IsActive, d.SortOrder,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrDefectNotFound
	}
	return nil
}

// Delete 软删除。唯一索引不含 deleted_at，软删后同 code 无法重建（MVP 可接受）。
func (r *PGDefectRepo) Delete(ctx context.Context, id int64) error {
	ct, err := r.pool.Exec(ctx,
		"UPDATE content_defects SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrDefectNotFound
	}
	return nil
}

// SetActive 启停。
func (r *PGDefectRepo) SetActive(ctx context.Context, id int64, active bool) error {
	ct, err := r.pool.Exec(ctx,
		"UPDATE content_defects SET is_active = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL", id, active)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrDefectNotFound
	}
	return nil
}

// Reorder 交换同项目下两个剧本的 sort_order。
func (r *PGDefectRepo) Reorder(ctx context.Context, projectID int64, id1, id2 int64) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE content_defects d SET
			sort_order = CASE d.id WHEN $2 THEN (SELECT sort_order FROM content_defects WHERE id = $3)
			                       WHEN $3 THEN (SELECT sort_order FROM content_defects WHERE id = $2) END,
			updated_at = now()
		WHERE d.id IN ($2, $3) AND d.project_id = $1 AND d.deleted_at IS NULL`,
		projectID, id1, id2)
	if err != nil {
		return err
	}
	if ct.RowsAffected() != 2 {
		return ErrDefectNotFound
	}
	return nil
}

// CountByProject 统计项目下未删除剧本数。
func (r *PGDefectRepo) CountByProject(ctx context.Context, projectID int64) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM content_defects WHERE project_id = $1 AND deleted_at IS NULL", projectID).Scan(&count)
	return count, err
}

// ——— 辅助 ———

// scannable 兼容 QueryRow 与 Rows。
func scanDefect(row scannable) (*model.Defect, error) {
	var d model.Defect
	var triggerJSON, manifestationJSON, fixJSON, amplifierJSON, freezeJSON []byte
	var provenanceJSON, bindingsJSON []byte
	var category, description, rootCause *string
	var targetServiceID *int64
	var estimatedFixTime *int
	err := row.Scan(
		&d.ID, &d.ProjectID, &d.Code, &d.Name, &d.DefectType, &category,
		&d.Difficulty, &description, &rootCause, &targetServiceID,
		&triggerJSON, &manifestationJSON, &fixJSON, &amplifierJSON, &freezeJSON,
		&d.ScoreWeight, &estimatedFixTime,
		&d.IsActive, &d.SortOrder, &d.CreatedAt, &d.UpdatedAt,
		&provenanceJSON, &bindingsJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDefectNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Category = derefString(category)
	d.Description = derefString(description)
	d.RootCause = derefString(rootCause)
	d.TargetServiceID = derefInt64(targetServiceID)
	d.EstimatedFixTime = derefInt(estimatedFixTime)
	_ = json.Unmarshal(triggerJSON, &d.TriggerConfig)
	_ = json.Unmarshal(manifestationJSON, &d.ManifestationConfig)
	_ = json.Unmarshal(fixJSON, &d.FixConfig)
	_ = json.Unmarshal(amplifierJSON, &d.AmplifierConfig)
	_ = json.Unmarshal(freezeJSON, &d.FreezeRules)
	if err := json.Unmarshal(provenanceJSON, &d.Provenance); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(bindingsJSON, &d.TemplateBindings); err != nil {
		return nil, err
	}
	return &d, nil
}

func jsonMarshalMap(m model.JSONMap) (string, error) {
	if m == nil {
		m = model.JSONMap{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("序列化 JSONB 失败: %w", err)
	}
	return string(b), nil
}

// jsonMarshalNullableMap 可空 JSONB：空 map 存 NULL（如 amplifier_config/freeze_rules）。
func jsonMarshalNullableMap(m model.JSONMap) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("序列化 JSONB 失败: %w", err)
	}
	return b, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func nullableInt(v int) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
