// Package repository sandbox-service 的 PG 持久化实现。
//
// PGStore 同时实现 service.Store（创建/调谐/TTL 回收用的 5 个基础方法）
// 与 service.MonitorStore（管理后台列表/指标/异常），由同一 *pgxpool.Pool 装配。
//
// 共库 MVP：sandboxes 表部分列（cluster_id/speed_multiplier/resource_profile 等）
// 当前领域模型未覆盖，写入走列默认值，读取只 SELECT 模型需要的列。
// uuid 列 NOT NULL UNIQUE 但 model 无对应字段，Create 时在此生成。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appresp "github.com/quenching/go-common/response"
	"github.com/quenching/sandbox-service/internal/model"
)

// 活跃状态：非 released、非 failed（与 model.Sandbox.IsActiveState 语义一致）。
const activeWhere = `status <> 'released' AND status <> 'failed' AND deleted_at IS NULL`

// ErrSandboxNotFound / ErrSandboxConflict 复用 service 域错误（同码同义）。
var (
	errSandboxNotFound = appresp.New(30004, "沙箱不存在")
	errSandboxConflict = appresp.New(30005, "沙箱状态冲突")
)

// PGStore 沙箱 PG 仓储。
type PGStore struct {
	pool *pgxpool.Pool
}

// 编译期断言：同时满足两个接口。
var (
	_ interface {
		ListActive(ctx context.Context) ([]model.Sandbox, error)
		ListAllForReap(ctx context.Context) ([]model.Sandbox, error)
		MarkStatus(ctx context.Context, sessionID int64, status string) error
		CountActiveByUser(ctx context.Context, userID int64) (int, error)
		Create(ctx context.Context, sb *model.Sandbox) error
	} = (*PGStore)(nil)
)

// NewPGStore 构造 PG 仓储。
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// sandboxColumns 模型需要的列。
// completed_at 裸取：未结束行是 NULL，由 NullTime 扫成零值，
// 切勿 COALESCE 成 to_timestamp(0)，否则 CompletedAt 变成 1970 非零值，
// 会让 IsExpired 的"结束后 5 分钟宽限"判定对所有沙箱立即成立。
// COALESCE(completed_at, destroyed_at)：老数据没有结束时刻，用销毁时刻兜底，
// 只影响已释放行的运行时长展示（已释放行不参与回收判定）。
// last_heartbeat_at 即"最后活动时间"，按活跃度续租的锚点。
const sandboxColumns = `id, COALESCE(session_id, 0), user_id, project_id,
	COALESCE(namespace, ''), status, COALESCE(access_url, ''),
	created_at, COALESCE(completed_at, destroyed_at), last_heartbeat_at, template_slug, template_digest, resource_plan, resources_admitted, allocation_reason`

func scanSandbox(row scannable) (model.Sandbox, error) {
	var sb model.Sandbox
	var completedAt, lastActivity NullTime
	if err := row.Scan(
		&sb.ID, &sb.SessionID, &sb.UserID, &sb.ProjectID,
		&sb.Namespace, &sb.Status, &sb.AccessURL,
		&sb.CreatedAt, &completedAt, &lastActivity, &sb.TemplateSlug, &sb.TemplateDigest, &sb.ResourcePlan, &sb.ResourcesAdmitted, &sb.AllocationReason,
	); err != nil {
		return model.Sandbox{}, err
	}
	if completedAt.Valid {
		sb.CompletedAt = completedAt.Time
	}
	if lastActivity.Valid {
		sb.LastActivityAt = lastActivity.Time
	}
	return sb, nil
}

// ——— service.Store 基础 5 方法 ———

// Create 新建沙箱记录，uuid 在此生成，namespace 缺失时按规则补齐。
func (s *PGStore) Create(ctx context.Context, sb *model.Sandbox) error {
	if sb.Namespace == "" && sb.SessionID > 0 {
		sb.Namespace = model.NamespaceName(sb.SessionID)
	}
	if sb.Status == "" {
		sb.Status = model.StatusCreating
	}
	const q = `
		INSERT INTO sandboxes (uuid, project_id, user_id, session_id, namespace, status, access_url)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))
		RETURNING id, created_at`
	err := s.pool.QueryRow(ctx, q,
		uuid.NewString(), sb.ProjectID, sb.UserID, sb.SessionID,
		sb.Namespace, sb.Status, sb.AccessURL,
	).Scan(&sb.ID, &sb.CreatedAt)
	if err != nil {
		return fmt.Errorf("插入沙箱记录失败: %w", err)
	}
	return nil
}

// ListActive 返回所有活跃（未终态）沙箱。
func (s *PGStore) ListActive(ctx context.Context) ([]model.Sandbox, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sandboxColumns+` FROM sandboxes WHERE `+activeWhere+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectSandboxes(rows)
}

// ListAllForReap 返回所有未释放沙箱（供 TTL 回收）。
func (s *PGStore) ListAllForReap(ctx context.Context) ([]model.Sandbox, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sandboxColumns+` FROM sandboxes
		 WHERE status <> 'released' AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectSandboxes(rows)
}

// MarkStatus 更新沙箱状态；置为 released 时回填 destroyed_at。
func (s *PGStore) MarkStatus(ctx context.Context, sessionID int64, status string) error {
	var tag pgconnCT
	var err error
	if status == model.StatusReleased {
		tag, err = s.pool.Exec(ctx,
			`UPDATE sandboxes SET status = $1, destroyed_at = now()
			 WHERE session_id = $2 AND deleted_at IS NULL`, status, sessionID)
	} else {
		tag, err = s.pool.Exec(ctx,
			`UPDATE sandboxes SET status = $1 WHERE session_id = $2 AND deleted_at IS NULL`,
			status, sessionID)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errSandboxNotFound.Unwrap()
	}
	return nil
}

// CountActiveByUser 统计指定用户的活跃沙箱数。
func (s *PGStore) CountActiveByUser(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM sandboxes WHERE user_id = $1 AND `+activeWhere,
		userID).Scan(&count)
	return count, err
}

// ——— 活跃度续租 / 结束回写 ———

// TouchActivity 刷新最后活动时间（按活跃度续租）。
// 只向后推进：并发写乱序时晚到的旧信号不会把租期往回拉。
// 命中已释放沙箱不报错（前端残帧、WS 重连后的迟到包属正常）。
func (s *PGStore) TouchActivity(ctx context.Context, sessionID int64, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sandboxes SET last_heartbeat_at = $2
		 WHERE session_id = $1 AND deleted_at IS NULL
		   AND status <> 'released'
		   AND (last_heartbeat_at IS NULL OR last_heartbeat_at < $2)`,
		sessionID, at)
	return err
}

// MarkCompleted 回写实训结束时刻，使沙箱进入结束宽限期（过后由回收扫描释放）。
// 已有结束时刻时保持首次值（幂等：弃班判定与用户点"退出"可能都调一次）。
func (s *PGStore) MarkCompleted(ctx context.Context, sessionID int64, at time.Time, reason string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sandboxes
		   SET completed_at = $2,
		       destroy_reason = CASE WHEN destroy_reason IS NULL OR destroy_reason = ''
		                             THEN NULLIF($3, '') ELSE destroy_reason END
		 WHERE session_id = $1 AND deleted_at IS NULL AND completed_at IS NULL`,
		sessionID, at, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists int
		_ = s.pool.QueryRow(ctx,
			`SELECT 1 FROM sandboxes WHERE session_id = $1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1`, sessionID).Scan(&exists)
		if exists == 0 {
			return errSandboxNotFound.Unwrap()
		}
	}
	return nil
}

// ——— MonitorStore 扩展方法 ———

// GetBySessionID 按会话ID获取沙箱；不存在返回 (nil, nil)（与调用方 nil 判断对齐）。
func (s *PGStore) GetBySessionID(ctx context.Context, sessionID int64) (*model.Sandbox, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+sandboxColumns+` FROM sandboxes
		 WHERE session_id = $1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1`, sessionID)
	sb, err := scanSandbox(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sb, nil
}

// GetByID 按主键获取沙箱；不存在返回 (nil, nil)。
func (s *PGStore) GetByID(ctx context.Context, id int64) (*model.Sandbox, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+sandboxColumns+` FROM sandboxes WHERE id = $1 AND deleted_at IS NULL`, id)
	sb, err := scanSandbox(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sb, nil
}

// List 分页 + 多维度筛选沙箱。
func (s *PGStore) List(ctx context.Context, q model.SandboxQuery) (*model.SandboxPage, error) {
	var conds []string
	var args []any
	conds = append(conds, "deleted_at IS NULL")
	if q.UserID > 0 {
		args = append(args, q.UserID)
		conds = append(conds, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if q.ProjectID > 0 {
		args = append(args, q.ProjectID)
		conds = append(conds, fmt.Sprintf("project_id = $%d", len(args)))
	}
	if q.Status != "" {
		args = append(args, q.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if q.Namespace != "" {
		args = append(args, "%"+q.Namespace+"%")
		conds = append(conds, fmt.Sprintf("namespace ILIKE $%d", len(args)))
	}
	if q.IsActive != nil {
		if *q.IsActive {
			conds = append(conds, activeWhere)
		} else {
			conds = append(conds, "status = 'released'")
		}
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM sandboxes WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, pageSize := normalizePage(q.Page, q.PageSize)
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.pool.Query(ctx,
		`SELECT `+sandboxColumns+` FROM sandboxes WHERE `+where+
			` ORDER BY created_at DESC, id DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := collectSandboxes(rows)
	if err != nil {
		return nil, err
	}
	return &model.SandboxPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// CountByStatus 按状态分组统计。
func (s *PGStore) CountByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT status, count(*) FROM sandboxes WHERE deleted_at IS NULL GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[status] = count
	}
	return result, rows.Err()
}

// CountActive 活跃沙箱总数。
func (s *PGStore) CountActive(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM sandboxes WHERE `+activeWhere).Scan(&count)
	return count, err
}

// Release 标记沙箱为 releasing（实际删 namespace 由调谐器执行）。
func (s *PGStore) Release(ctx context.Context, sessionID int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sandboxes SET status = 'releasing'
		 WHERE session_id = $1 AND status <> 'released' AND status <> 'releasing' AND deleted_at IS NULL`,
		sessionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// 区分"不存在"与"状态冲突"，与内存版行为对齐。
		var exists int
		_ = s.pool.QueryRow(ctx,
			`SELECT 1 FROM sandboxes WHERE session_id = $1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1`, sessionID).Scan(&exists)
		if exists == 0 {
			return errSandboxNotFound.Unwrap()
		}
		return errSandboxConflict.Unwrap()
	}
	return nil
}

// ——— 资源指标 ———

// SaveMetrics 保存一条指标快照。
func (s *PGStore) SaveMetrics(ctx context.Context, m *model.ResourceMetrics) error {
	// 时间零值（Go 的 0001 年）在 SQL 侧无法用 NULLIF($n, now()) 兜住，
	// 统一在 Go 侧补 now，避免写入非法/越界时间。
	ts := m.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sandbox_metrics
			(session_id, namespace, cpu_cores_m, cpu_percent, memory_bytes, memory_percent,
			 pod_count, pod_running, pod_abnormal, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		m.SessionID, m.Namespace, m.CPUCoresM, m.CPUPercent, m.MemoryBytes, m.MemoryPercent,
		m.PodCount, m.PodRunning, m.PodAbnormal, ts)
	return err
}

// GetLatestMetrics 获取最新指标；无数据返回 (nil, nil)。
func (s *PGStore) GetLatestMetrics(ctx context.Context, sessionID int64) (*model.ResourceMetrics, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT session_id, namespace, cpu_cores_m, cpu_percent, memory_bytes, memory_percent,
		       pod_count, pod_running, pod_abnormal, created_at
		FROM sandbox_metrics WHERE session_id = $1 ORDER BY id DESC LIMIT 1`, sessionID)
	m, err := scanMetrics(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListMetrics 返回最近 limit 条指标，按时间升序（与内存版切片顺序一致）。
func (s *PGStore) ListMetrics(ctx context.Context, sessionID int64, limit int) ([]model.ResourceMetrics, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT * FROM (
			SELECT session_id, namespace, cpu_cores_m, cpu_percent, memory_bytes, memory_percent,
			       pod_count, pod_running, pod_abnormal, created_at
			FROM sandbox_metrics WHERE session_id = $1 ORDER BY id DESC LIMIT $2
		) t ORDER BY t.created_at ASC, t.session_id ASC`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.ResourceMetrics
	for rows.Next() {
		m, err := scanMetrics(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// ——— 异常告警 ———

// RecordAnomaly 记录一条异常，回填 ID/检测时间。
func (s *PGStore) RecordAnomaly(ctx context.Context, a *model.SandboxAnomaly) error {
	snapshot, err := json.Marshal(a.Snapshot)
	if err != nil {
		return fmt.Errorf("序列化异常快照失败: %w", err)
	}
	// 检测时间零值在 Go 侧兜底（SQL 侧 NULLIF($n, now()) 拦不住 0001 年）。
	detectedAt := a.DetectedAt
	if detectedAt.IsZero() {
		detectedAt = time.Now()
	}
	// 未解决（ResolvedAt 零值）时 resolved_at 必须写 NULL，
	// 由 NullTime 表示 Valid=false；绝不能写 to_timestamp(0)。
	var resolvedAt NullTime
	if !a.ResolvedAt.IsZero() {
		resolvedAt = NullTime{Time: a.ResolvedAt, Valid: true}
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO sandbox_anomalies
			(session_id, namespace, user_id, anomaly_type, level, message, snapshot,
			 resolved, detected_at, resolved_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, detected_at`,
		a.SessionID, a.Namespace, a.UserID, a.AnomalyType, string(a.Level), a.Message,
		json.RawMessage(snapshot), a.Resolved, detectedAt, resolvedAt,
	).Scan(&a.ID, &a.DetectedAt)
}

// ListAnomalies 分页查询异常。
func (s *PGStore) ListAnomalies(ctx context.Context, q model.AnomalyQuery) (*model.AnomalyPage, error) {
	var conds []string
	var args []any
	if q.Level != "" {
		args = append(args, string(q.Level))
		conds = append(conds, fmt.Sprintf("level = $%d", len(args)))
	}
	if q.AnomalyType != "" {
		args = append(args, q.AnomalyType)
		conds = append(conds, fmt.Sprintf("anomaly_type = $%d", len(args)))
	}
	if q.Resolved != nil {
		args = append(args, *q.Resolved)
		conds = append(conds, fmt.Sprintf("resolved = $%d", len(args)))
	}
	where := "TRUE"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM sandbox_anomalies WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, pageSize := normalizePage(q.Page, q.PageSize)
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, namespace, user_id, anomaly_type, level, message,
		       COALESCE(snapshot, 'null'::jsonb), resolved, detected_at, resolved_at
		FROM sandbox_anomalies WHERE `+where+
		` ORDER BY detected_at DESC, id DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.SandboxAnomaly, 0)
	for rows.Next() {
		var a model.SandboxAnomaly
		var snapshot []byte
		var resolvedAt NullTime
		if err := rows.Scan(
			&a.ID, &a.SessionID, &a.Namespace, &a.UserID, &a.AnomalyType, &a.Level, &a.Message,
			&snapshot, &a.Resolved, &a.DetectedAt, &resolvedAt,
		); err != nil {
			return nil, err
		}
		if len(snapshot) > 0 && string(snapshot) != "null" {
			var snap model.ResourceMetrics
			if err := json.Unmarshal(snapshot, &snap); err == nil {
				a.Snapshot = &snap
			}
		}
		if resolvedAt.Valid {
			a.ResolvedAt = resolvedAt.Time
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &model.AnomalyPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// ResolveAnomaly 标记异常已解决。
func (s *PGStore) ResolveAnomaly(ctx context.Context, anomalyID int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sandbox_anomalies SET resolved = true, resolved_at = now()
		 WHERE id = $1 AND resolved = false`, anomalyID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("异常记录不存在或已解决")
	}
	return nil
}

// CountUnresolvedAnomalies 未解决异常数。
func (s *PGStore) CountUnresolvedAnomalies(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM sandbox_anomalies WHERE resolved = false`).Scan(&count)
	return count, err
}
