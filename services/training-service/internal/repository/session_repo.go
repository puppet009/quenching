// 训练会话仓储（个人中心 / 人机对照真接口）。
//
// 承载会话生命周期的读写：
//   - 创建会话（显式 status=pending，含交叉分组第二场的 session_defects 挂故障）
//   - 开始 / 结束会话（条件 UPDATE 落实状态机与时间戳）
//   - 操作上报（training_operation_projection 批量写入，回写 total_operation_count）
//   - 历史列表（本地 SQL 只查本服务表，项目 / 故障名称经内部 HTTP 端口回填）
//   - 概览聚合（计数 / 时长 / 均分 / 最近雷达）
//   - 人机对照聚合（配对两会话的指标、技术雷达与 reviews 协作子分）
//
// projects / defects 等跨服务表不再 JOIN：名称类数据经 content-service /
// fault-engine 的 /internal/v1 只读端口批量获取；协作雷达经 user-service
// /internal/v1 只读端口获取；reviews 仍为共库只读 JOIN，待第四批拆离。
//
// 注意：仓储交互 DTO 统一定义在 service 包（service 不能导入本包，否则与本包
// 既有的 repository→service 依赖构成 import cycle），本文件只做端口适配实现。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	oe "github.com/quenching/proto/operationevidence"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// SessionRepo 训练会话仓储。
type SessionRepo struct {
	pool       *pgxpool.Pool
	defects    service.DefectReader
	content    service.ContentReader
	users      service.UserProfileStore
	reviews    service.ReviewStore
	operations OperationWriter
}

// NewSessionRepo 创建训练会话仓储。
//   - defects 为 fault-engine 故障目录只读端口：把 defects 表 JOIN 替换为
//     “本地先查首故障 ID + 批量回填名称”；
//   - content 为 content-service 项目目录只读端口：把 projects 表 JOIN 替换为
//     “本地先查 project_id + 批量回填名称”；
//   - users 为 user-service 协作档案只读端口：把 user_profiles 表直查替换为
//     内部 HTTP 查询；
//   - reviews 为 review-service 复盘协作数据端口：把 reviews 表 JOIN/直查替换为
//     批量存在性回填与单会话子分查询。
func NewSessionRepo(
	pool *pgxpool.Pool,
	defects service.DefectReader,
	content service.ContentReader,
	users service.UserProfileStore,
	reviews service.ReviewStore,
) *SessionRepo {
	return &SessionRepo{
		pool:    pool,
		defects: defects,
		content: content,
		users:   users,
		reviews: reviews,
	}
}

// enrichProjectNames 按本服务侧查到的项目 ID 批量回填名称。
// 调用失败或端口缺失时保守降级为空名（不误报、不阻断主流程）。
func (r *SessionRepo) enrichProjectNames(ctx context.Context, rows []service.SessionHistoryRow) error {
	if r.content == nil || len(rows) == 0 {
		return nil
	}
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for i := range rows {
		id := rows[i].ProjectID
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	views, err := r.content.BatchProjects(ctx, ids)
	if err != nil {
		// 降级：名称留空，列表主体仍可用。
		return nil
	}
	names := make(map[int64]string, len(views))
	for i := range views {
		names[views[i].ID] = views[i].Name
	}
	for i := range rows {
		if rows[i].ProjectID > 0 {
			rows[i].ProjectName = names[rows[i].ProjectID]
		}
	}
	return nil
}

// enrichDefectNames 按本服务侧查到的首故障 ID 批量回填名称。
// 调用失败或端口缺失时保守降级为占位名（不误报、不阻断主流程）。
func (r *SessionRepo) enrichDefectNames(ctx context.Context, rows []service.SessionHistoryRow) error {
	if r.defects == nil || len(rows) == 0 {
		return nil
	}
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for i := range rows {
		id := rows[i].FirstDefectID
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	views, err := r.defects.BatchDefects(ctx, ids)
	if err != nil {
		// 降级：名称留空占位，列表主体仍可用。
		return nil
	}
	names := make(map[int64]string, len(views))
	for i := range views {
		names[views[i].ID] = views[i].Name
	}
	for i := range rows {
		if rows[i].FirstDefectID > 0 {
			rows[i].DefectName = names[rows[i].FirstDefectID]
		}
	}
	return nil
}

// 编译期断言：SessionRepo 实现 service.SessionStore。
var _ service.SessionStore = (*SessionRepo)(nil)

// ——— 创建 ———

// CreateSession 插入一条 pending 会话。attempt2（in.DefectID>0）时同事务
// 在 session_defects 挂接校准故障（status 默认 dormant）。返回新会话 ID。
func (r *SessionRepo) CreateSession(ctx context.Context, in service.CreateSessionInput) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO training_sessions
		    (tenant_id, user_id, project_id, template_slug, title, difficulty, ai_mode,
		     control_group, status)
		VALUES (0, $1, NULLIF($2, 0), NULLIF($3, ''), NULLIF($4, ''), $5, $6, NULLIF($7, ''), 'pending')
		RETURNING id
	`, in.UserID, in.ProjectID, in.TemplateSlug, in.Title, in.Difficulty, in.AIMode, in.ControlGroup).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("插入训练会话: %w", err)
	}

	if in.DefectID > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO session_defects (tenant_id, session_id, defect_id)
			VALUES (0, $1, $2)
		`, id, in.DefectID)
		if err != nil {
			return 0, fmt.Errorf("挂接校准故障: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// ——— 开始 / 结束（条件 UPDATE 落实状态机）———

// StartSession 把会话从 pending/deploying/armed 推进到 running，并记录开始时间。
// 采用单一条件 UPDATE：MVP 没有独立部署回调，pending 直接合法进入 running。
// 返回是否命中（false 表示会话不存在 / 不属于该用户 / 当前状态不允许开始）。
func (r *SessionRepo) StartSession(ctx context.Context, sessionID, userID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE training_sessions
		   SET status='running',
		       started_at=COALESCE(started_at, now()),
		       updated_at=now()
		 WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL
		   AND status IN ('pending','deploying','armed')
	`, sessionID, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// FinishSession 条件 UPDATE 把活动态会话推进到终态并记录结束时间。
// 返回是否命中（false 表示不存在 / 无权 / 已结束 / 目标状态非法）。
func (r *SessionRepo) FinishSession(ctx context.Context, in service.FinishSessionInput) (bool, error) {
	if model.SessionStatus(in.Status) != model.StatusCompleted &&
		model.SessionStatus(in.Status) != model.StatusAborted &&
		model.SessionStatus(in.Status) != model.StatusFailed {
		return false, fmt.Errorf("非法结束状态 %q", in.Status)
	}

	var durationSec any
	if in.Duration != nil {
		durationSec = int64(in.Duration.Seconds())
	}

	tag, err := r.pool.Exec(ctx, `
		UPDATE training_sessions
		   SET status=$3,
		       fault_resolved=$4,
		       actual_duration=CASE
		           WHEN $5::bigint IS NOT NULL THEN $5
		           WHEN started_at IS NOT NULL
		                THEN EXTRACT(EPOCH FROM (now() - started_at))::bigint
		           ELSE actual_duration
		       END,
		       ended_at=COALESCE(ended_at, now()),
		       updated_at=now()
		 WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL
		   AND status IN ('pending','deploying','armed','running','resolved')
	`, in.SessionID, in.UserID, in.Status, in.Resolved, durationSec)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// AbandonSession 系统判定弃班：活跃会话（armed/running/resolved）置为 aborted 且未解决。
// 不带 user_id 条件：调用方是卡顿扫描器（服务侧定时任务），会话来自它自己列出的活跃集合。
// 已是终态时不命中（false），避免覆盖用户在同一瞬间提交的正常结束。
func (r *SessionRepo) AbandonSession(ctx context.Context, sessionID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE training_sessions
		   SET status='aborted',
		       fault_resolved=false,
		       actual_duration=CASE
		           WHEN started_at IS NOT NULL
		            THEN EXTRACT(EPOCH FROM (now() - started_at))::bigint
		           ELSE actual_duration
		       END,
		       ended_at=COALESCE(ended_at, now()),
		       updated_at=now()
		 WHERE id=$1 AND deleted_at IS NULL
		   AND status IN ('armed','running','resolved')
	`, sessionID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// GetSessionStatus 读取会话当前状态与归属；不存在返回 ("", false, nil)。
func (r *SessionRepo) GetSessionStatus(ctx context.Context, sessionID, userID int64) (string, bool, error) {
	var status string
	err := r.pool.QueryRow(ctx, `
		SELECT status FROM training_sessions
		 WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL
	`, sessionID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}

// ——— 操作上报 ———

// AddOperations 批量写入操作，回写 total_operation_count。
// 会话须属于该用户且未删除（不限制状态，迟到事件允许落库）。
// 返回写入条数；会话不存在 / 无权时返回 (0, service.ErrSessionNotFound)。
func (r *SessionRepo) AddOperations(ctx context.Context, sessionID, userID int64, ops []service.OperationInput) (int, error) {
	if len(ops) == 0 {
		return 0, nil
	}
	var owned bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM training_sessions WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)`, sessionID, userID).Scan(&owned)
	if err != nil {
		return 0, err
	}
	if !owned {
		return 0, service.ErrSessionNotFound
	}
	if r.operations == nil {
		return 0, fmt.Errorf("sandbox operation writer unavailable")
	}
	receipt, err := r.operations.Record(ctx, sessionID, userID, ops)
	if err != nil {
		return 0, err
	}
	_, err = r.pool.Exec(ctx, `UPDATE training_sessions SET total_operation_count=GREATEST(total_operation_count,$2),updated_at=now() WHERE id=$1 AND user_id=$3`, sessionID, receipt.Total, userID)
	if err != nil {
		return 0, fmt.Errorf("operations saved; counter update unconfirmed: %w", err)
	}
	return receipt.Written, nil
}

func marshalDetail(detail any) ([]byte, error) {
	if detail == nil {
		return nil, nil
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return nil, fmt.Errorf("序列化操作 detail: %w", err)
	}
	if string(b) == "null" {
		return nil, nil
	}
	return b, nil
}

// ——— 历史列表 ———

// ListSessions 分页查询当前用户的训练历史。本地 SQL 只查本服务表（含
// LATERAL 取首故障 ID），项目 / 故障名称在结果取回后经内部端口批量回填；
// 端口不可用时名称留空，不影响主行。故障取 session_defects 中 id 最小的一个。
func (r *SessionRepo) ListSessions(ctx context.Context, f service.SessionListFilter) ([]service.SessionHistoryRow, int64, error) {
	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	where := []string{"s.user_id = $1", "s.deleted_at IS NULL"}
	args := []any{f.UserID}
	idx := 2
	if f.Status == "finished" {
		where = append(where, "s.status IN ('completed','aborted','failed')")
	} else if f.Status != "" {
		return nil, 0, fmt.Errorf("unsupported session history status")
	}

	if f.Difficulty != "" {
		where = append(where, fmt.Sprintf("s.difficulty = $%d", idx))
		args = append(args, f.Difficulty)
		idx++
	}
	if f.AIMode != "" {
		where = append(where, fmt.Sprintf("s.ai_mode = $%d", idx))
		args = append(args, f.AIMode)
		idx++
	}
	if f.Success != nil {
		where = append(where, fmt.Sprintf("s.fault_resolved = $%d", idx))
		args = append(args, *f.Success)
		idx++
	}
	if strings.TrimSpace(f.Keyword) != "" {
		kw := strings.TrimSpace(f.Keyword)
		// 拆库后名称 ILIKE 改为双反查：content-service 项目名 + fault-engine
		// 故障名，再在本地用 project_id / 首故障 ID 对齐。反查失败的分支直接
		// 丢弃（宁可漏匹配，也绝不放大结果集）；两者都不可用 / 均无匹配时，
		// keyword 条件置为永假而不是放宽为全量。
		var projectIDs, defectIDs []int64
		if r.content != nil {
			if views, searchErr := r.content.SearchProjectByName(ctx, kw); searchErr == nil {
				for _, v := range views {
					projectIDs = append(projectIDs, v.ID)
				}
			}
		}
		if r.defects != nil {
			if views, searchErr := r.defects.SearchDefectByName(ctx, kw); searchErr == nil {
				for _, v := range views {
					defectIDs = append(defectIDs, v.ID)
				}
			}
		}
		switch {
		case len(projectIDs) > 0 && len(defectIDs) > 0:
			where = append(where,
				fmt.Sprintf("(s.project_id = ANY($%d) OR sd.defect_id = ANY($%d))", idx, idx+1))
			args = append(args, projectIDs, defectIDs)
			idx += 2
		case len(projectIDs) > 0:
			where = append(where, fmt.Sprintf("s.project_id = ANY($%d)", idx))
			args = append(args, projectIDs)
			idx++
		case len(defectIDs) > 0:
			where = append(where, fmt.Sprintf("sd.defect_id = ANY($%d)", idx))
			args = append(args, defectIDs)
			idx++
		default:
			where = append(where, "FALSE")
		}
	}

	whereSQL := strings.Join(where, " AND ")

	var total int64
	countSQL := `
		SELECT count(*)
		  FROM training_sessions s
		  LEFT JOIN LATERAL (
		      SELECT defect_id FROM session_defects
		       WHERE session_id = s.id AND deleted_at IS NULL
		       ORDER BY id LIMIT 1
		  ) sd ON true
		 WHERE ` + whereSQL
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, pageSize, (page-1)*pageSize)
	listSQL := `
		SELECT s.id,
		       COALESCE(s.project_id, 0),
		       COALESCE(sd.defect_id, 0),
		       s.difficulty,
		       s.ai_mode,
		       s.tech_score,
		       s.collab_score,
		       s.fault_resolved,
		       COALESCE(s.actual_duration, 0),
		       s.started_at,
		       s.created_at,
		       s.status,
		       COALESCE(b.phase, ''),
		       COALESCE(b.cleanup_status, '')
		  FROM training_sessions s
		  LEFT JOIN brain_sessions b ON b.session_id = s.id
		  LEFT JOIN LATERAL (
		      SELECT defect_id FROM session_defects
		       WHERE session_id = s.id AND deleted_at IS NULL
		       ORDER BY id LIMIT 1
		  ) sd ON true
		 WHERE ` + whereSQL + `
		 ORDER BY s.created_at DESC, s.id DESC
		 LIMIT $` + fmt.Sprint(idx) + ` OFFSET $` + fmt.Sprint(idx+1)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]service.SessionHistoryRow, 0, pageSize)
	for rows.Next() {
		var row service.SessionHistoryRow
		if err := rows.Scan(
			&row.ID, &row.ProjectID, &row.FirstDefectID, &row.Difficulty, &row.AIMode,
			&row.TechScore, &row.CollabScore, &row.Success, &row.DurationSec,
			&row.StartTime, &row.CreatedAt, &row.Status, &row.BrainPhase, &row.CleanupStatus,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.enrichDefectNames(ctx, out); err != nil {
		// 回填内部已保守降级；此处不阻断返回。
		_ = err
	}
	if err := r.enrichProjectNames(ctx, out); err != nil {
		_ = err
	}
	if err := r.enrichReviewExistence(ctx, out); err != nil {
		_ = err
	}
	return out, total, nil
}

// enrichReviewExistence 按本页会话 ID 批量回填 HasReview / ReviewID。
// 调用失败或端口缺失时保守降级为 false / nil（不误报、不阻断主流程）。
func (r *SessionRepo) enrichReviewExistence(ctx context.Context, rows []service.SessionHistoryRow) error {
	// A failed lookup is unknown, not proof that a report has not been created.
	for i := range rows {
		rows[i].ReviewLookupFailed = true
	}
	if r.reviews == nil || len(rows) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		if rows[i].ID > 0 {
			ids = append(ids, rows[i].ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	items, err := r.reviews.ListReviewExistence(ctx, ids)
	if err != nil {
		// 降级：HasReview 保持 false，列表主体仍可用。
		return nil
	}
	reviewIDs := make(map[int64]int64, len(items))
	for i := range items {
		if items[i].ReviewID != nil {
			reviewIDs[items[i].SessionID] = *items[i].ReviewID
		}
	}
	for i := range rows {
		rows[i].ReviewLookupFailed = false
		if id, ok := reviewIDs[rows[i].ID]; ok {
			idCopy := id
			rows[i].HasReview = true
			rows[i].ReviewID = &idCopy
		}
	}
	return nil
}

// ——— 详情 ———

// GetSessionDetail 查单个会话详情（按 id + user_id 鉴权），JOIN 口径与
// ListSessions 一致；无记录返回 (nil, nil)。
func (r *SessionRepo) GetSessionDetail(ctx context.Context, sessionID, userID int64) (*service.SessionDetail, error) {
	var detail service.SessionDetail
	var firstDefectID int64
	err := r.pool.QueryRow(ctx, `
		SELECT s.id,
		       s.status,
		       s.ai_mode,
		       COALESCE(s.control_group, ''),
		       s.difficulty,
		       COALESCE(s.project_id, 0),
		       COALESCE(sd.defect_id, 0),
		       s.started_at,
		       s.created_at
		  FROM training_sessions s
		  LEFT JOIN LATERAL (
		      SELECT defect_id FROM session_defects
		       WHERE session_id = s.id AND deleted_at IS NULL
		       ORDER BY id LIMIT 1
		  ) sd ON true
		 WHERE s.id = $1
		   AND s.user_id = $2
		   AND s.deleted_at IS NULL`, sessionID, userID).Scan(
		&detail.ID, &detail.Status, &detail.AIMode, &detail.ControlGroup,
		&detail.Difficulty, &detail.ProjectID, &firstDefectID,
		&detail.StartTime, &detail.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// 跨域回填项目名称：单次批量调用承载单个 ID；失败保守留空。
	if detail.ProjectID > 0 && r.content != nil {
		if views, cerr := r.content.BatchProjects(ctx, []int64{detail.ProjectID}); cerr == nil && len(views) > 0 {
			detail.ProjectName = views[0].Name
		}
	}
	// 跨域回填首故障名称；失败保守留空，不影响详情主体。
	if firstDefectID > 0 && r.defects != nil {
		if views, ferr := r.defects.BatchDefects(ctx, []int64{firstDefectID}); ferr == nil && len(views) > 0 {
			detail.DefectName = views[0].Name
		}
	}
	return &detail, nil
}

// GetMetrics 概览计数：总会话、已结束、已修复、总训练时长（秒）。
func (r *SessionRepo) GetMetrics(ctx context.Context, userID int64) (service.MetricsAggregate, error) {
	var m service.MetricsAggregate
	err := r.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status IN ('completed','aborted','failed')),
		       count(*) FILTER (WHERE fault_resolved = true),
		       COALESCE(sum(COALESCE(actual_duration, 0)), 0)
		  FROM training_sessions
		 WHERE user_id = $1 AND deleted_at IS NULL
	`, userID).Scan(&m.TotalSessions, &m.Finished, &m.Success, &m.TotalDuration)
	return m, err
}

// GetScoreStats 返回已落分会话的技术 / 协作均分（无数据时对应字段为 nil）。
func (r *SessionRepo) GetScoreStats(ctx context.Context, userID int64) (service.ScoreStats, error) {
	var s service.ScoreStats
	err := r.pool.QueryRow(ctx, `
		SELECT avg(tech_score) FILTER (WHERE tech_score IS NOT NULL),
		       avg(collab_score) FILTER (WHERE collab_score IS NOT NULL)
		  FROM training_sessions
		 WHERE user_id = $1 AND deleted_at IS NULL
	`, userID).Scan(&s.AvgTechScore, &s.AvgCollabScore)
	return s, err
}

// LatestScoredRadar 返回最近一条具备 tech_radar 的已结束会话的技术雷达，
// 以及该用户在 user-service 中的协作雷达（可能均为空）。
func (r *SessionRepo) LatestScoredRadar(ctx context.Context, userID int64) (tech, collab model.RadarData, err error) {
	var techJSON *string
	err = r.pool.QueryRow(ctx, `
		SELECT tech_radar
		  FROM training_sessions
		 WHERE user_id = $1 AND deleted_at IS NULL
		   AND tech_radar IS NOT NULL AND status IN ('completed','resolved')
		 ORDER BY ended_at DESC NULLS LAST, id DESC
		 LIMIT 1
	`, userID).Scan(&techJSON)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// 无技术雷达
	case err != nil:
		return tech, collab, err
	default:
		if techJSON != nil && *techJSON != "" {
			_ = json.Unmarshal([]byte(*techJSON), &tech)
		}
	}

	if r.users != nil {
		profile, profileErr := r.users.GetCollabProfile(ctx, userID)
		switch {
		case profileErr != nil:
			// 协作档案服务不可用时保守降级为空协作雷达，不影响本地技术雷达。
			return tech, collab, nil
		case profile != nil && profile.Radar != nil:
			collab = *profile.Radar
		}
	}
	return tech, collab, nil
}

// ——— 人机对照 ———

// GetComparisonPair 取用户最近一对双向配对（pair_session_id 互指）的两会话，
// 按 ai_mode 归类返回。无配对时返回 (nil, nil, nil)。
func (r *SessionRepo) GetComparisonPair(
	ctx context.Context, userID int64,
) (manual *service.ComparisonSessionRow, aiCollab *service.ComparisonSessionRow, err error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.ai_mode, s.difficulty, s.fault_resolved,
		       COALESCE(s.actual_duration, 0),
		       s.tech_score, s.collab_score,
		       COALESCE(s.pitfall_total, 0),
		       COALESCE(s.pitfall_recognized, 0),
		       COALESCE(sd.defect_id, 0)
		  FROM training_sessions s
		  LEFT JOIN LATERAL (
		      SELECT defect_id FROM session_defects
		       WHERE session_id = s.id AND deleted_at IS NULL
		       ORDER BY id LIMIT 1
		  ) sd ON true
		 WHERE s.user_id = $1 AND s.deleted_at IS NULL
		   AND s.pair_session_id IS NOT NULL
		   AND EXISTS (
		       SELECT 1 FROM training_sessions p
			WHERE p.id = s.pair_session_id
			  AND p.pair_session_id = s.id
			  AND p.user_id = $1 AND p.deleted_at IS NULL)
		 ORDER BY s.id DESC
		 LIMIT 2
	`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	// 先收集行与首故障 ID，再批量回填名称（至多 2 行，一次批量调用）。
	type pairRow struct {
		row      service.ComparisonSessionRow
		defectID int64
	}
	collected := make([]pairRow, 0, 2)
	for rows.Next() {
		var pr pairRow
		if err := rows.Scan(
			&pr.row.ID, &pr.row.AIMode, &pr.row.Difficulty, &pr.row.Success, &pr.row.DurationSec,
			&pr.row.TechScore, &pr.row.CollabScore, &pr.row.PitfallTotal, &pr.row.PitfallRecognized,
			&pr.defectID,
		); err != nil {
			return nil, nil, err
		}
		collected = append(collected, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	nameByID := make(map[int64]string)
	if r.defects != nil {
		idSet := make(map[int64]struct{})
		ids := make([]int64, 0, len(collected))
		for _, pr := range collected {
			if pr.defectID > 0 {
				if _, ok := idSet[pr.defectID]; !ok {
					idSet[pr.defectID] = struct{}{}
					ids = append(ids, pr.defectID)
				}
			}
		}
		if len(ids) > 0 {
			if views, ferr := r.defects.BatchDefects(ctx, ids); ferr == nil {
				for i := range views {
					nameByID[views[i].ID] = views[i].Name
				}
			}
		}
	}

	for _, pr := range collected {
		pr.row.DefectName = nameByID[pr.defectID]
		switch model.AIMode(pr.row.AIMode) {
		case model.AIModeNone:
			if manual == nil || pr.row.ID > manual.ID {
				v := pr.row
				manual = &v
			}
		case model.AIModeAssist:
			if aiCollab == nil || pr.row.ID > aiCollab.ID {
				v := pr.row
				aiCollab = &v
			}
		}
	}
	return manual, aiCollab, nil
}

// GetSessionReviewRow 读取单会话技术雷达，并经 review-service 内部接口回填协作子分。
// reviews 无行或内部接口失败时协作子分全部为 nil（技术雷达仍可取）。会话不存在返回 (nil,nil)。
func (r *SessionRepo) GetSessionReviewRow(
	ctx context.Context, sessionID int64,
) (*service.SessionReviewRow, error) {
	var row service.SessionReviewRow
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.tech_radar
		  FROM training_sessions s
		 WHERE s.id = $1 AND s.deleted_at IS NULL
	`, sessionID).Scan(&row.ID, &row.TechRadarJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.reviews != nil {
		rv, rvErr := r.reviews.GetReviewBySession(ctx, sessionID)
		if rvErr == nil && rv != nil {
			row.QuestionQuality = rv.QuestionQuality
			row.InfoUtilization = rv.InfoUtilization
			row.Judgment = rv.Judgment
			row.ErrorRecognitionRate = rv.ErrorRecognitionRate
			row.EvidenceValidationRate = rv.EvidenceValidationRate
		}
	}
	return &row, nil
}

// CountSessionOperations 统计单会话操作指标：危险操作数、观察类操作数、AI 提问数。
func (r *SessionRepo) CountSessionOperations(
	ctx context.Context, sessionID int64,
) (dangerous, observe, aiQuestions int64, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT
		    count(*) FILTER (WHERE COALESCE(penalty, 0) > 0 OR result = 'failed'),
		    count(*) FILTER (WHERE operation_category = 'observe'),
		    count(*) FILTER (WHERE operation_type IN ('ask_ai','ai_question','follow_ai'))
		  FROM training_operation_projection
		 WHERE session_id = $1 AND deleted_at IS NULL
	`, sessionID).Scan(&dangerous, &observe, &aiQuestions)
	return dangerous, observe, aiQuestions, err
}

type OperationWriter interface {
	Record(context.Context, int64, int64, []service.OperationInput) (oe.LegacyReceipt, error)
}

func (r *SessionRepo) WithOperationWriter(w OperationWriter) *SessionRepo { r.operations = w; return r }
