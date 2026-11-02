// Package repository 卡顿检测的数据端口实现（T3-6）。
//
// StallRepo 实现 service.StallPort：
//   - running 会话与开训时间取自 training_sessions；
//   - 有效活动双来源：training_operation_projection（T1-23 写入，可能尚未合入）
//     与 ai_messages 中用户主动提问（JOIN ai_conversations 按 session_id），
//     同一时刻产生的记录只计一次；写入方未上线时由后者/started_at 兜底；
//   - 指标观察对象为会话挂载缺陷的目标业务服务（defects.target_service_id →
//     content-service.project_services.name），多服务时按时间戳对齐取均值，
//     与 metrics 包"多序列取平均"的口径一致。
//
// 本文件全程只读，不建表、不写字段、不推送。
package repository

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/pkg/metrics"
	"github.com/quenching/training-service/internal/service"
)

// 编译期保证实现 service.StallPort。
var _ service.StallPort = (*StallRepo)(nil)

// MetricQuerier metrics.Service 的查询能力子集，便于替身测试。
type MetricQuerier interface {
	QueryMetric(ctx context.Context, req metrics.QueryRequest) (*metrics.QueryResult, error)
}

// StallRepo 卡顿扫描器的 PG + Prometheus 数据端口。
type StallRepo struct {
	pool    *pgxpool.Pool
	metrics MetricQuerier
	defects service.DefectReader
	content service.ContentReader
}

// NewStallRepo 创建卡顿数据端口。
//   - defects 为 fault-engine 故障目录只读端口：本地查 defect_id 后批量取
//     target_service_id；
//   - content 为 content-service 目录只读端口：target_service_id 批量解析为
//     project_services.name。
func NewStallRepo(
	pool *pgxpool.Pool, mq MetricQuerier,
	defects service.DefectReader, content service.ContentReader,
) *StallRepo {
	return &StallRepo{pool: pool, metrics: mq, defects: defects, content: content}
}

// ListRunning 列出全部 running（且未删除）会话及其开训时间、归属与项目。
// user_id / project_id 供弃班时触发出"未完成"复盘（复盘生成入参需要两者）。
func (r *StallRepo) ListRunning(ctx context.Context) ([]model.StallSessionInfo, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, project_id, started_at
		 FROM training_sessions
		 WHERE status='running' AND deleted_at IS NULL
		 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.StallSessionInfo, 0)
	for rows.Next() {
		var s model.StallSessionInfo
		var started *time.Time
		if err := rows.Scan(&s.ID, &s.UserID, &s.ProjectID, &started); err != nil {
			return nil, err
		}
		if started != nil {
			s.StartedAt = *started
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// activitySQL 有效活动双来源：
//   - training_operation_projection 任意未删除行（T1-23 写入）；
//   - ai_messages 中 role='user' 的提问，经 ai_conversations 关联到会话。
//
// 两路 UNION 后同一 created_at 只计一次（DISTINCT），与"有效活动=一次用户行为"的口径一致；
// 最外层对空集也返回一行（max=NULL, count=0），调用方无需处理无行情况。
const activitySQL = `
SELECT max(ts), count(*) FROM (
	SELECT created_at AS ts
	FROM training_operation_projection
	WHERE session_id=$1 AND deleted_at IS NULL AND created_at >= $2 AND created_at <= $3
	UNION
	SELECT m.created_at AS ts
	FROM ai_messages m
	JOIN ai_conversations c ON c.id = m.conversation_id
	WHERE c.session_id=$1 AND m.role='user' AND m.created_at >= $2 AND m.created_at <= $3
) a`

// Activity 取窗口内最近活动时间与活动次数；从无活动时 last=nil、count=0。
// 时间窗口按闭区间 [since, now] 过滤；扫描器再以 detector 的空闲判定兜底。
func (r *StallRepo) Activity(
	ctx context.Context, sessionID int64, since, now time.Time,
) (*time.Time, int, error) {
	var last *time.Time
	var count int
	if err := r.pool.QueryRow(ctx, activitySQL, sessionID, since, now).Scan(&last, &count); err != nil {
		return nil, 0, err
	}
	return last, count, nil
}

// sessionServices 取会话挂载缺陷涉及的目标业务服务名（去重、升序）。
//
// 拆库链路（跨三服务，全程内部 HTTP 只读）：
//  1. 本服务表 session_defects 本地查出挂载的 defect_id；
//  2. 经 fault-engine 内部接口批量取 defects.target_service_id；
//  3. 经 content-service 内部接口批量解析为 project_services.name。
//
// 任一步失败：返回错误，由 MetricWindows 上游按保守语义弃权。
func (r *StallRepo) sessionServices(ctx context.Context, sessionID int64) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT defect_id
		 FROM session_defects
		 WHERE session_id=$1 AND deleted_at IS NULL
		 ORDER BY defect_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defectIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		defectIDs = append(defectIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(defectIDs) == 0 || r.defects == nil {
		return []string{}, nil
	}

	views, err := r.defects.BatchDefects(ctx, defectIDs)
	if err != nil {
		return nil, err
	}
	serviceIDSet := make(map[int64]struct{})
	serviceIDs := make([]int64, 0)
	for i := range views {
		sid := views[i].TargetServiceID
		if sid <= 0 {
			continue
		}
		if _, ok := serviceIDSet[sid]; ok {
			continue
		}
		serviceIDSet[sid] = struct{}{}
		serviceIDs = append(serviceIDs, sid)
	}
	if len(serviceIDs) == 0 {
		return []string{}, nil
	}
	if r.content == nil {
		// 端口缺失视为数据不可用：返回错误让上游保守弃权，不凭空名误判。
		return nil, errors.New("content-service 只读端口未装配")
	}

	svcViews, err := r.content.BatchServiceNames(ctx, serviceIDs)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(svcViews))
	for i := range svcViews {
		if svcViews[i].Name == "" {
			continue
		}
		names = append(names, svcViews[i].Name)
	}
	// 内部端口按 ID 排序返回；这里恢复改前“按名升序”的输出口径。
	slices.Sort(names)
	return names, nil
}

// MetricWindows 对会话挂载缺陷的全部目标业务服务查询三指标窗口并合并。
//   - 无目标服务：返回三个 Available=false 窗口（detector 对 R2 弃权）；
//   - 任一服务查询失败：该项 Available=false（保守弃权，绝不凭查询失败误报）；
//   - 多服务：按毫秒时间戳对齐取均值，与 metrics.HTTPPromClient 多序列口径一致。
func (r *StallRepo) MetricWindows(
	ctx context.Context, sessionID int64, start, end time.Time,
) (model.StallMetricWindows, error) {
	svcs, err := r.sessionServices(ctx, sessionID)
	if err != nil {
		return model.StallMetricWindows{}, err
	}

	w := model.StallMetricWindows{}
	if len(svcs) == 0 {
		// 无目标服务属于正常数据形态（如会话尚未挂载缺陷），不视为端口错误，
		// 以不可用窗口让 detector 弃权。
		return w, nil
	}

	w.ErrorRate = r.queryMerged(ctx, metrics.MetricErrorRate, svcs, start, end)
	w.P99Latency = r.queryMerged(ctx, metrics.MetricP99Latency, svcs, start, end)
	w.QPS = r.queryMerged(ctx, metrics.MetricQPS, svcs, start, end)
	return w, nil
}

// queryMerged 查询单个指标在多个服务上的窗口并按时间戳合并取均值。
// 任一服务查询失败即整体置 Available=false（保守原则）。
func (r *StallRepo) queryMerged(
	ctx context.Context, name metrics.MetricName, svcs []string, start, end time.Time,
) model.StallMetricWindow {
	perTS := make(map[int64][]float64)
	for _, svc := range svcs {
		res, err := r.metrics.QueryMetric(ctx, metrics.QueryRequest{
			Metric:      name,
			Service:     svc,
			Start:       start,
			End:         end,
			Step:        15,
			Aggregation: metrics.AggAvg,
		})
		if err != nil {
			return model.StallMetricWindow{Available: false}
		}
		for _, p := range res.Points {
			perTS[p.Timestamp] = append(perTS[p.Timestamp], p.Value)
		}
	}
	if len(perTS) == 0 {
		// 查询成功但无点（服务无流量/窗口太新）：置可用但空，由 detector 按"点不足"弃权，
		// 与"查询失败"区分，便于云端验证时观察弃权原因。
		return model.StallMetricWindow{Available: true, Points: nil}
	}

	tsList := make([]int64, 0, len(perTS))
	for ts := range perTS {
		tsList = append(tsList, ts)
	}
	slices.Sort(tsList)

	points := make([]model.StallMetricPoint, 0, len(tsList))
	for _, ts := range tsList {
		vs := perTS[ts]
		sum := 0.0
		for _, v := range vs {
			sum += v
		}
		points = append(points, model.StallMetricPoint{TimestampMS: ts, Value: sum / float64(len(vs))})
	}
	return model.StallMetricWindow{Available: true, Points: points}
}
