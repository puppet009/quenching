// service.ReviewInputStore 的 PostgreSQL 实现。
//
// 本地只查本服务表（training_sessions / training_operation_projection / session_defects /
// ai_messages / ai_conversations / ai_pitfall_configs）；剧本答案列 root_cause
// 不落在本服务，由 queryScripts 在服务端批量调 fault-engine 剧本详情端点回填，
// 缺详情的缺陷 ID 直接报错，避免生成没有预设根因的空洞剧本。
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// PGInternalReviewInputRepo 复盘聚合 PG 仓储。
type PGInternalReviewInputRepo struct {
	pool     *pgxpool.Pool
	scripts  service.DefectScriptReader
	pitfalls PitfallConfigReader
}

// NewPGInternalReviewInputRepo 创建复盘聚合仓储。
// scripts 用于回填剧本详情（root_cause）；为 nil 时剧本聚合会返回明确错误。
func NewPGInternalReviewInputRepo(
	pool *pgxpool.Pool, scripts service.DefectScriptReader,
) *PGInternalReviewInputRepo {
	return &PGInternalReviewInputRepo{pool: pool, scripts: scripts}
}

// 编译期断言：满足 service.ReviewInputStore。
var _ service.ReviewInputStore = (*PGInternalReviewInputRepo)(nil)

// GetReviewInput 聚合复盘主输入：元数据（含存在性校验）先行，时间线与剧本并发。
func (r *PGInternalReviewInputRepo) GetReviewInput(
	ctx context.Context, sessionID int64,
) (*service.SessionReviewInput, error) {
	var difficulty string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(difficulty, '')
		FROM training_sessions
		WHERE id = $1 AND deleted_at IS NULL`, sessionID).Scan(&difficulty)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query training_sessions: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)
	var timeline []service.ReviewTimelineItem
	var scripts []service.ReviewSessionScript
	g.Go(func() error {
		var qErr error
		timeline, qErr = r.queryTimeline(gctx, sessionID)
		return qErr
	})
	g.Go(func() error {
		var qErr error
		scripts, qErr = r.queryScripts(gctx, sessionID)
		return qErr
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	in := &service.SessionReviewInput{
		SessionID:  sessionID,
		Difficulty: difficulty,
		Timeline:   timeline,
		Scripts:    scripts,
	}
	if len(scripts) > 0 {
		in.FaultType = scripts[0].Category
	}
	return in, nil
}

// queryTimeline 查操作时间线，按相对时间升序（NULL 排末尾）。
func (r *PGInternalReviewInputRepo) queryTimeline(
	ctx context.Context, sessionID int64,
) ([]service.ReviewTimelineItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT virtual_timestamp, operation_type,
		       operation_category, target, result
		FROM training_operation_projection
		WHERE session_id = $1 AND deleted_at IS NULL
		ORDER BY virtual_timestamp NULLS LAST, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query training_operation_projection: %w", err)
	}
	defer rows.Close()

	items := []service.ReviewTimelineItem{}
	for rows.Next() {
		var (
			vt               *int
			category, target *string
			opType, result   string
		)
		if err := rows.Scan(&vt, &opType, &category, &target, &result); err != nil {
			return nil, fmt.Errorf("scan training_operation_projection: %w", err)
		}
		item := service.ReviewTimelineItem{Type: opType}
		if result != "" {
			item.Result = result
		}
		if vt != nil {
			item.OffsetSec = *vt
		}
		if category != nil {
			item.Category = *category
		}
		if target != nil {
			item.Target = *target
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate training_operation_projection: %w", err)
	}
	return items, nil
}

// sessionScriptRow session_defects 本地状态行（剧本详情回填前）。
type sessionScriptRow struct {
	defectID                  int64
	status                    string
	timeToIdentify, timeToFix *int
}

// queryScripts 查本次会话剧本：本地取 session_defects 状态，剧本详情批量回填。
func (r *PGInternalReviewInputRepo) queryScripts(
	ctx context.Context, sessionID int64,
) ([]service.ReviewSessionScript, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT defect_id, status, time_to_identify, time_to_fix
		FROM session_defects
		WHERE session_id = $1 AND deleted_at IS NULL
		ORDER BY triggered_at NULLS LAST, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session_defects: %w", err)
	}
	defer rows.Close()

	local := []sessionScriptRow{}
	for rows.Next() {
		var row sessionScriptRow
		if err := rows.Scan(&row.defectID, &row.status,
			&row.timeToIdentify, &row.timeToFix); err != nil {
			return nil, fmt.Errorf("scan session_defects: %w", err)
		}
		local = append(local, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session_defects: %w", err)
	}
	if len(local) == 0 {
		return []service.ReviewSessionScript{}, nil
	}
	if r.scripts == nil {
		return nil, errors.New("review input repo: defect script reader is not initialized")
	}

	ids := make([]int64, 0, len(local))
	for i := range local {
		ids = append(ids, local[i].defectID)
	}
	details, err := r.scripts.ListScriptDetails(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("fetch defect script details: %w", err)
	}
	detailByID := make(map[int64]service.FaultScriptView, len(details))
	for i := range details {
		detailByID[details[i].ID] = details[i]
	}

	scripts := make([]service.ReviewSessionScript, 0, len(local))
	for i := range local {
		detail, ok := detailByID[local[i].defectID]
		if !ok {
			return nil, fmt.Errorf("defect %d: script detail missing", local[i].defectID)
		}
		s := service.ReviewSessionScript{
			DefectID:    detail.ID,
			Code:        detail.Code,
			Name:        detail.Name,
			Category:    detail.Category,
			Difficulty:  detail.Difficulty,
			Description: detail.Description,
			RootCause:   detail.RootCause,
			Status:      local[i].status,
		}
		if local[i].timeToIdentify != nil {
			s.TimeToIdentify = *local[i].timeToIdentify
		}
		if local[i].timeToFix != nil {
			s.TimeToFix = *local[i].timeToFix
		}
		scripts = append(scripts, s)
	}
	return scripts, nil
}

// ListChanges 查助手全部文件修改记录（相对会话开始时间，时间正序）。
func (r *PGInternalReviewInputRepo) ListChanges(
	ctx context.Context, sessionID int64,
) ([]service.ReviewChangeItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT cr.id, cr.pod, cr.container, cr.path,
		       cr.before_existed, COALESCE(cr.before_content, ''),
		       cr.after_content,
		       EXTRACT(EPOCH FROM (cr.created_at - s.created_at))::int
		FROM ai_change_records cr
		JOIN training_sessions s ON s.id = cr.session_id AND s.deleted_at IS NULL
		WHERE cr.session_id = $1 AND cr.deleted_at IS NULL
		ORDER BY cr.created_at, cr.id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query ai_change_records: %w", err)
	}
	defer rows.Close()

	items := []service.ReviewChangeItem{}
	for rows.Next() {
		var item service.ReviewChangeItem
		if err := rows.Scan(
			&item.ID, &item.Pod, &item.Container, &item.Path,
			&item.BeforeExisted, &item.BeforeContent, &item.AfterContent,
			&item.OffsetSec,
		); err != nil {
			return nil, fmt.Errorf("scan ai_change_records: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ai_change_records: %w", err)
	}
	return items, nil
}

// ListPitfalls 查会话中全部 AI 坑点原始数据（4 表 JOIN，均为本服务表）。
func (r *PGInternalReviewInputRepo) ListPitfalls(
	ctx context.Context, sessionID int64,
) ([]service.ReviewPitfallRawItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			m.id,
			COALESCE(m.pitfall_config_id, 0),
			COALESCE(m.pitfall_type, ''),
			COALESCE(m.user_response_type, 'pending'),
			'',
			'',
			'',
			'',
			'medium',
			EXTRACT(EPOCH FROM (m.created_at - s.created_at))::int
		FROM ai_messages m
		JOIN ai_conversations c ON c.id = m.conversation_id AND c.deleted_at IS NULL
		JOIN training_sessions s ON s.id = c.session_id AND s.deleted_at IS NULL
		WHERE c.session_id = $1
		  AND m.is_pitfall = true
		  AND m.role = 'assistant'
		ORDER BY m.created_at, m.id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session pitfalls: %w", err)
	}
	defer rows.Close()

	items := []service.ReviewPitfallRawItem{}
	for rows.Next() {
		var (
			item      service.ReviewPitfallRawItem
			offsetSec *int
		)
		if err := rows.Scan(
			&item.MessageID, &item.PitfallConfigID, &item.PitfallType,
			&item.UserResponse, &item.Name, &item.Description,
			&item.ErrorContent, &item.CorrectApproach, &item.Severity,
			&offsetSec,
		); err != nil {
			return nil, fmt.Errorf("scan pitfall item: %w", err)
		}
		if offsetSec != nil {
			item.AppearOffsetSec = *offsetSec
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pitfall items: %w", err)
	}
	if len(items) > 0 && r.pitfalls == nil {
		return nil, fmt.Errorf("content pitfall reader unavailable")
	}
	for i := range items {
		if items[i].PitfallConfigID <= 0 {
			continue
		}
		row, e := r.pitfalls.GetByID(ctx, items[i].PitfallConfigID)
		if e != nil {
			return nil, e
		}
		items[i].Name = row.Name
		items[i].Description = row.Description
		items[i].ErrorContent = row.ErrorContent
		items[i].CorrectApproach = row.CorrectApproach
		items[i].Severity = row.Severity
	}

	return items, nil
}

type PitfallConfigReader interface {
	GetByID(context.Context, int64) (*model.PitfallStoredRow, error)
}

func (r *PGInternalReviewInputRepo) WithPitfalls(p PitfallConfigReader) *PGInternalReviewInputRepo {
	r.pitfalls = p
	return r
}
