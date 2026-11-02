// Package repository training-service 的 PostgreSQL 仓储实现。
// 坑点配置仓储：训练启动时只读加载 active 配置（T3-8/9），
// 后台管理端提供 CRUD（T3-18 坑点配置后端接口）。
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/content-service/internal/model"
)

// ErrPitfallConfigNotFound 记录不存在（model.ErrPitfallNotFound 的别名）。
// 归位编排层是为消除 service↔repository 依赖环；保留别名使 handler/tests 既有引用不破坏。
var ErrPitfallConfigNotFound = model.ErrPitfallNotFound

// 编译期保证实现 service.Store。

// PitfallRepo 坑点配置只读仓储。
type PitfallRepo struct {
	pool *pgxpool.Pool
}

func NewPitfallRepo(pool *pgxpool.Pool) *PitfallRepo {
	return &PitfallRepo{pool: pool}
}

// ListActiveByDefectIDs 取多个缺陷下所有 active 坑点配置（含三块 JSONB 原始字节）。
// #nosec G201 -- 表名固定，无外部拼接。
func (r *PitfallRepo) ListActiveByDefectIDs(ctx context.Context, defectIDs []int64) ([]model.PitfallStoredRow, error) {
	if len(defectIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, defect_id, pitfall_type, name, COALESCE(description,''),
		        trigger_condition, error_content, COALESCE(confidence_level,'random'),
		        recognition_rules, pitfall_rules, COALESCE(correct_approach,''),
		        COALESCE(severity,'medium'), status, sort_order,
		        COALESCE(created_by,0), created_at, updated_at
		 FROM content_ai_pitfall_configs
		 WHERE status = 'active' AND defect_id = ANY($1)
		 ORDER BY defect_id, sort_order, id`,
		defectIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.PitfallStoredRow, 0)
	for rows.Next() {
		var row model.PitfallStoredRow
		if err := rows.Scan(
			&row.ID, &row.DefectID, &row.PitfallType, &row.Name, &row.Description,
			&row.TriggerCondition, &row.ErrorContent, &row.ConfidenceLevel,
			&row.RecognitionRules, &row.PitfallRules, &row.CorrectApproach,
			&row.Severity, &row.Status, &row.SortOrder,
			&row.CreatedBy, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ---------- 后台管理 CRUD（T3-18） ----------

// 类型归位编排层（避免 service→repository 反向依赖环），此处保留别名兼容既有引用。
type (
	// PitfallQuery 坑点列表查询参数。
	PitfallQuery = model.PitfallQuery
	// StoredPage 仓储层分页结果（原始行）；service 负责解析为对外的 service.PitfallPage。
	StoredPage = model.StoredPage
)

// GetByID 按 ID 查询单条配置。
func (r *PitfallRepo) GetByID(ctx context.Context, id int64) (*model.PitfallStoredRow, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, defect_id, pitfall_type, name, COALESCE(description,''),
		        trigger_condition, error_content, COALESCE(confidence_level,'random'),
		        recognition_rules, pitfall_rules, COALESCE(correct_approach,''),
		        COALESCE(severity,'medium'), status, sort_order,
		        COALESCE(created_by,0), created_at, updated_at
		 FROM content_ai_pitfall_configs
		 WHERE id = $1 AND deleted_at IS NULL`, id)

	var out model.PitfallStoredRow
	err := row.Scan(
		&out.ID, &out.DefectID, &out.PitfallType, &out.Name, &out.Description,
		&out.TriggerCondition, &out.ErrorContent, &out.ConfidenceLevel,
		&out.RecognitionRules, &out.PitfallRules, &out.CorrectApproach,
		&out.Severity, &out.Status, &out.SortOrder,
		&out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// List 分页查询坑点配置（后台管理用）。返回原始行，由 service 层解析校验。
func (r *PitfallRepo) List(ctx context.Context, q PitfallQuery) (*StoredPage, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}

	where := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	argIdx := 1

	if q.DefectID > 0 {
		where = append(where, "defect_id = $"+strconv.Itoa(argIdx))
		args = append(args, q.DefectID)
		argIdx++
	}
	if q.PitfallType != "" {
		where = append(where, "pitfall_type = $"+strconv.Itoa(argIdx))
		args = append(args, q.PitfallType)
		argIdx++
	}
	if q.Status != "" {
		where = append(where, "status = $"+strconv.Itoa(argIdx))
		args = append(args, q.Status)
		argIdx++
	}

	whereSQL := strings.Join(where, " AND ")

	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM content_ai_pitfall_configs WHERE `+whereSQL,
		args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("count pitfall configs: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	limitSQL := " ORDER BY sort_order, id LIMIT $" + strconv.Itoa(argIdx) + " OFFSET $" + strconv.Itoa(argIdx+1)
	args = append(args, q.PageSize, offset)

	rows, err := r.pool.Query(ctx,
		`SELECT id, defect_id, pitfall_type, name, COALESCE(description,''),
		        trigger_condition, error_content, COALESCE(confidence_level,'random'),
		        recognition_rules, pitfall_rules, COALESCE(correct_approach,''),
		        COALESCE(severity,'medium'), status, sort_order,
		        COALESCE(created_by,0), created_at, updated_at
		 FROM content_ai_pitfall_configs
		 WHERE `+whereSQL+limitSQL,
		args...)
	if err != nil {
		return nil, fmt.Errorf("list pitfall configs: %w", err)
	}
	defer rows.Close()

	items := make([]model.PitfallStoredRow, 0)
	for rows.Next() {
		var row model.PitfallStoredRow
		if err := rows.Scan(
			&row.ID, &row.DefectID, &row.PitfallType, &row.Name, &row.Description,
			&row.TriggerCondition, &row.ErrorContent, &row.ConfidenceLevel,
			&row.RecognitionRules, &row.PitfallRules, &row.CorrectApproach,
			&row.Severity, &row.Status, &row.SortOrder,
			&row.CreatedBy, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pitfall row: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pitfall rows: %w", err)
	}

	return &StoredPage{
		Total:    total,
		Page:     q.Page,
		PageSize: q.PageSize,
		Items:    items,
	}, nil
}

// Create 创建坑点配置，返回带 ID 的 StoredRow。
func (r *PitfallRepo) Create(ctx context.Context, cfg *model.PitfallConfig, createdBy int64) (*model.PitfallStoredRow, error) {
	triggerJSON, err := json.Marshal(cfg.Trigger)
	if err != nil {
		return nil, fmt.Errorf("marshal trigger_condition: %w", err)
	}
	recognitionJSON, err := json.Marshal(cfg.Recognition)
	if err != nil {
		return nil, fmt.Errorf("marshal recognition_rules: %w", err)
	}
	pitfallRulesJSON, err := json.Marshal(cfg.PitfallRules)
	if err != nil {
		return nil, fmt.Errorf("marshal pitfall_rules: %w", err)
	}

	status := cfg.Status
	if status == "" {
		status = model.StatusDraft
	}
	confidence := cfg.ConfidenceLevel
	if confidence == "" {
		confidence = model.ConfidenceRandom
	}
	severity := cfg.Severity
	if severity == "" {
		severity = model.SeverityMedium
	}

	var id int64
	var createdAt, updatedAt time.Time
	err = r.pool.QueryRow(ctx,
		`INSERT INTO content_ai_pitfall_configs
			(defect_id, pitfall_type, name, description,
			 trigger_condition, error_content, confidence_level,
			 recognition_rules, pitfall_rules, correct_approach,
			 severity, status, sort_order, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id, created_at, updated_at`,
		cfg.DefectID, cfg.PitfallType, cfg.Name, cfg.Description,
		triggerJSON, cfg.ErrorContent, confidence,
		recognitionJSON, pitfallRulesJSON, cfg.CorrectApproach,
		severity, status, cfg.SortOrder, createdBy,
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert pitfall config: %w", err)
	}

	return &model.PitfallStoredRow{
		ID:               id,
		DefectID:         cfg.DefectID,
		PitfallType:      cfg.PitfallType,
		Name:             cfg.Name,
		Description:      cfg.Description,
		TriggerCondition: triggerJSON,
		ErrorContent:     cfg.ErrorContent,
		ConfidenceLevel:  confidence,
		RecognitionRules: recognitionJSON,
		PitfallRules:     pitfallRulesJSON,
		CorrectApproach:  cfg.CorrectApproach,
		Severity:         severity,
		Status:           status,
		SortOrder:        cfg.SortOrder,
		CreatedBy:        createdBy,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}, nil
}

// Update 更新坑点配置。
func (r *PitfallRepo) Update(ctx context.Context, id int64, cfg *model.PitfallConfig) error {
	triggerJSON, err := json.Marshal(cfg.Trigger)
	if err != nil {
		return fmt.Errorf("marshal trigger_condition: %w", err)
	}
	recognitionJSON, err := json.Marshal(cfg.Recognition)
	if err != nil {
		return fmt.Errorf("marshal recognition_rules: %w", err)
	}
	pitfallRulesJSON, err := json.Marshal(cfg.PitfallRules)
	if err != nil {
		return fmt.Errorf("marshal pitfall_rules: %w", err)
	}

	result, err := r.pool.Exec(ctx,
		`UPDATE content_ai_pitfall_configs SET
			defect_id = $1,
			pitfall_type = $2,
			name = $3,
			description = $4,
			trigger_condition = $5,
			error_content = $6,
			confidence_level = $7,
			recognition_rules = $8,
			pitfall_rules = $9,
			correct_approach = $10,
			severity = $11,
			status = $12,
			sort_order = $13,
			updated_at = now()
		 WHERE id = $14 AND deleted_at IS NULL`,
		cfg.DefectID, cfg.PitfallType, cfg.Name, cfg.Description,
		triggerJSON, cfg.ErrorContent, cfg.ConfidenceLevel,
		recognitionJSON, pitfallRulesJSON, cfg.CorrectApproach,
		cfg.Severity, cfg.Status, cfg.SortOrder, id,
	)
	if err != nil {
		return fmt.Errorf("update pitfall config: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrPitfallConfigNotFound
	}
	return nil
}

// UpdateStatus 更新配置状态（启用/禁用/草稿）。
func (r *PitfallRepo) UpdateStatus(ctx context.Context, id int64, status string) error {
	result, err := r.pool.Exec(ctx,
		`UPDATE content_ai_pitfall_configs SET status = $1, updated_at = now()
		 WHERE id = $2 AND deleted_at IS NULL`,
		status, id)
	if err != nil {
		return fmt.Errorf("update pitfall status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrPitfallConfigNotFound
	}
	return nil
}

// Delete 软删除坑点配置。
func (r *PitfallRepo) Delete(ctx context.Context, id int64) error {
	result, err := r.pool.Exec(ctx,
		`UPDATE content_ai_pitfall_configs SET deleted_at = now(), updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete pitfall config: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrPitfallConfigNotFound
	}
	return nil
}
