// training-service 装配适配器（T2-33 端到端联调）。
//
// 各业务能力（service 包内告警引擎/健康卡片/日志观察/恢复验证）通过端口接口解耦，
// 本文件作为组合根（composition root），把 metrics/logquery/service 告警/eventbus
// 的具体实现适配到这些端口上。适配器不含业务逻辑，仅做签名与 DTO 映射。
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/pkg/eventbus"
	"github.com/quenching/training-service/internal/pkg/logquery"
	"github.com/quenching/training-service/internal/pkg/metrics"
	"github.com/quenching/training-service/internal/service"
)

// ========== 告警数据源适配（metrics/logquery → service 告警引擎） ==========

// alertMetricsAdapter 把 metrics.Service 适配到 service.AlertMetricsSource。
//
// 注意：metrics 侧沙箱隔离由 PromQLBuilder 的 namespace+releaseName 保证，
// 因此 sandboxID 形参在此忽略（metrics 查询请求本身不含 sandboxID 维度）。
type alertMetricsAdapter struct {
	svc *metrics.Service
}

func (a *alertMetricsAdapter) GetCurrentValue(ctx context.Context, sandboxID, service, metric, aggregation string) (float64, error) {
	res, err := a.svc.GetCurrentValue(ctx, metrics.InstantQueryRequest{
		Metric:      metrics.MetricName(metric),
		Service:     service,
		At:          time.Now(),
		Aggregation: metrics.Aggregation(aggregation),
	})
	if err != nil {
		return 0, err
	}
	return res.Value, nil
}

func (a *alertMetricsAdapter) QueryRange(ctx context.Context, sandboxID, svcName, metric string, start, end time.Time, step int) ([]service.AlertDataPoint, error) {
	res, err := a.svc.QueryMetric(ctx, metrics.QueryRequest{
		Metric:  metrics.MetricName(metric),
		Service: svcName,
		Start:   start,
		End:     end,
		Step:    step,
	})
	if err != nil {
		return nil, err
	}
	points := make([]service.AlertDataPoint, 0, len(res.Points))
	for _, p := range res.Points {
		points = append(points, service.AlertDataPoint{Timestamp: p.Timestamp, Value: p.Value})
	}
	return points, nil
}

// alertLogsAdapter 把 logquery.Service 适配到 service.AlertLogSource。
type alertLogsAdapter struct {
	svc *logquery.Service
}

func (a *alertLogsAdapter) CountKeyword(ctx context.Context, sandboxID, service, keyword, level string, start, end time.Time) (int, error) {
	return a.svc.CountKeyword(ctx, sandboxID, service, keyword, logquery.LogLevel(level), start, end)
}

// 编译期断言：两个适配器满足 service 告警引擎端口。
var (
	_ service.AlertMetricsSource = (*alertMetricsAdapter)(nil)
	_ service.AlertLogSource     = (*alertLogsAdapter)(nil)
)

// ========== 健康卡片数据源适配（metrics/alert → 健康卡片） ==========

// healthMetricsAdapter 把 metrics.Service 适配到 service.HealthCardMetricsProvider（值类型返回）。
type healthMetricsAdapter struct {
	svc *metrics.Service
}

func (a *healthMetricsAdapter) CheckServiceHealth(ctx context.Context, svcName string) (service.ServiceHealth, error) {
	h, err := a.svc.CheckServiceHealth(ctx, svcName)
	if err != nil {
		return service.ServiceHealth{}, err
	}
	return service.ServiceHealth{
		Service:     h.Service,
		Healthy:     h.Healthy,
		CPUUsage:    h.CPUUsage,
		MemoryUsage: h.MemoryUsage,
		ErrorRate:   h.ErrorRate,
		Reason:      h.Reason,
	}, nil
}

func (a *healthMetricsAdapter) GetAllMetrics(ctx context.Context, svcName string) (service.AllMetricsSnapshot, error) {
	snap, err := a.svc.GetAllMetrics(ctx, svcName)
	if err != nil {
		return service.AllMetricsSnapshot{}, err
	}
	conv := func(m metrics.MetricSnapshotItem) service.MetricValue {
		return service.MetricValue{Value: m.Value, Unit: m.Unit, DisplayName: m.DisplayName}
	}
	return service.AllMetricsSnapshot{
		CPUUsage:    conv(snap.CPUUsage),
		MemoryUsage: conv(snap.MemoryUsage),
		QPS:         conv(snap.QPS),
		ErrorRate:   conv(snap.ErrorRate),
		P99Latency:  conv(snap.P99Latency),
		Timestamp:   snap.Timestamp,
	}, nil
}

// healthAlertsAdapter 把 service.AlertManager 适配到 service.HealthCardAlertProvider。
//
// Manager.ListAlerts 不按沙箱过滤，这里在内存中按 AlertInstance.SandboxID 过滤，
// 仅统计活跃告警（firing + acknowledged）。
type healthAlertsAdapter struct {
	mgr *service.AlertManager
}

func isActiveAlert(s service.AlertStatus) bool {
	return s == service.AlertStatusFiring || s == service.AlertStatusAcknowledged
}

func (a *healthAlertsAdapter) ListAlerts(sandboxID string, status string) []service.AlertInfo {
	var alerts []*service.AlertInstance
	if status == "" {
		alerts = a.mgr.ListAlerts(service.AlertStatus(""))
	} else {
		alerts = a.mgr.ListAlerts(service.AlertStatus(status))
	}

	result := make([]service.AlertInfo, 0, len(alerts))
	for _, al := range alerts {
		if al.SandboxID != sandboxID {
			continue
		}
		result = append(result, service.AlertInfo{
			ID:       al.ID,
			RuleID:   al.RuleID,
			RuleName: al.RuleName,
			Severity: string(al.AlertSeverity),
			Service:  al.Service,
			Message:  al.Message,
			Status:   string(al.Status),
		})
	}
	return result
}

func (a *healthAlertsAdapter) AlertCountByService(sandboxID string) map[string]service.AlertCount {
	counts := make(map[string]service.AlertCount)
	for _, al := range a.mgr.ListAlerts(service.AlertStatus("")) {
		if al.SandboxID != sandboxID || !isActiveAlert(al.Status) {
			continue
		}
		c := counts[al.Service]
		c.Total++
		switch al.AlertSeverity {
		case service.AlertSeverityCritical:
			c.Critical++
		case service.AlertSeverityWarning:
			c.Warning++
		case service.AlertSeverityInfo:
			c.Info++
		}
		counts[al.Service] = c
	}
	return counts
}

var (
	_ service.HealthCardMetricsProvider = (*healthMetricsAdapter)(nil)
	_ service.HealthCardAlertProvider   = (*healthAlertsAdapter)(nil)
)

// ========== 事件发布适配（eventbus.Publisher → 裸 string 单方法端口） ==========

// channelPublisher 把 eventbus.Publisher 适配到健康卡片/日志观察的
// 裸 string EventPublisher（具名 EventType → string 的转换包装）。
//
// inner 为 nil 时（本地未部署 ws-gateway）所有发布静默丢弃；
// 两个消费方对 publisher 均有 nil 守卫，但本包装也做 nil 防御。
type channelPublisher struct {
	inner eventbus.Publisher
}

func (p *channelPublisher) PublishToChannel(ctx context.Context, channel, eventType string, data interface{}) error {
	if p.inner == nil {
		return nil
	}
	return p.inner.PublishToChannel(ctx, channel, eventbus.EventType(eventType), data)
}

var _ service.EventPublisher = (*channelPublisher)(nil)

// ========== 日志观察适配（logquery → 日志观察） ==========

// viewerLogsAdapter 把 logquery.Service 适配到 service.LogViewerLogProvider。
type viewerLogsAdapter struct {
	svc *logquery.Service
}

func (a *viewerLogsAdapter) QueryLogs(ctx context.Context, sandboxID, svcName, level, keyword string,
	start, end time.Time, limit int, direction string) (*service.LogQueryResultRaw, error) {

	res, err := a.svc.QueryLogs(ctx, logquery.LogQueryRequest{
		SandboxID: sandboxID,
		Service:   svcName,
		Level:     logquery.LogLevel(level),
		Keyword:   keyword,
		StartTime: start,
		EndTime:   end,
		Limit:     limit,
		Direction: logquery.QueryDirection(direction),
	})
	if err != nil {
		return nil, err
	}
	raw := &service.LogQueryResultRaw{Total: res.Total, HasMore: res.HasMore}
	raw.Entries = make([]service.LogEntryRaw, 0, len(res.Entries))
	for _, e := range res.Entries {
		raw.Entries = append(raw.Entries, service.LogEntryRaw{
			Timestamp: e.Timestamp,
			Line:      e.Line,
			Labels:    e.Labels,
		})
	}
	return raw, nil
}

func (a *viewerLogsAdapter) GetLatestLogs(ctx context.Context, sandboxID, svcName string, limit int) (*service.LogQueryResultRaw, error) {
	res, err := a.svc.GetLatestLogs(ctx, sandboxID, svcName, limit)
	if err != nil {
		return nil, err
	}
	raw := &service.LogQueryResultRaw{Total: res.Total, HasMore: res.HasMore}
	raw.Entries = make([]service.LogEntryRaw, 0, len(res.Entries))
	for _, e := range res.Entries {
		raw.Entries = append(raw.Entries, service.LogEntryRaw{
			Timestamp: e.Timestamp,
			Line:      e.Line,
			Labels:    e.Labels,
		})
	}
	return raw, nil
}

func (a *viewerLogsAdapter) Count(ctx context.Context, sandboxID, service, level string, start, end time.Time) (int, error) {
	return a.svc.Count(ctx, sandboxID, service, logquery.LogLevel(level), start, end)
}

// CountGrouped 按标签分组统计。logviewer 端口以 groupLabel 区分统计维度，
// 这里显式 switch 到 logquery 的两个具名方法，未知标签返回错误而非透传，
// 避免把不受控的标签名拼进 LogQL。
func (a *viewerLogsAdapter) CountGrouped(ctx context.Context, sandboxID, service, groupLabel string,
	start, end time.Time) (map[string]int, error) {

	switch groupLabel {
	case "level":
		return a.svc.CountGroupedByLevel(ctx, sandboxID, service, start, end)
	case "service":
		return a.svc.CountGroupedByService(ctx, sandboxID, start, end)
	default:
		return nil, fmt.Errorf("不支持的分组标签: %s", groupLabel)
	}
}

var _ service.LogViewerLogProvider = (*viewerLogsAdapter)(nil)

// ========== 恢复验证数据源适配（无 ctx 端口） ==========

// recoveryMetricsAdapter 把 metrics.Service 适配到 service.RecoveryMetricsProvider。
//
// recovery 端口方法不带 ctx，适配器内部用 context.Background()。
type recoveryMetricsAdapter struct {
	svc *metrics.Service
}

func (a *recoveryMetricsAdapter) GetServiceHealth(svcName string) (service.ServiceHealth, error) {
	h, err := a.svc.CheckServiceHealth(context.Background(), svcName)
	if err != nil {
		return service.ServiceHealth{}, err
	}
	return service.ServiceHealth{
		Service:     h.Service,
		Healthy:     h.Healthy,
		CPUUsage:    h.CPUUsage,
		MemoryUsage: h.MemoryUsage,
		ErrorRate:   h.ErrorRate,
		Reason:      h.Reason,
	}, nil
}

func (a *recoveryMetricsAdapter) GetMetricValue(service, metric string) (float64, error) {
	res, err := a.svc.GetCurrentValue(context.Background(), metrics.InstantQueryRequest{
		Metric:      metrics.MetricName(metric),
		Service:     service,
		At:          time.Now(),
		Aggregation: metrics.AggAvg,
	})
	if err != nil {
		return 0, err
	}
	return res.Value, nil
}

// recoveryAlertsAdapter 把 service.AlertManager 适配到 service.RecoveryAlertProvider（无 ctx）。
type recoveryAlertsAdapter struct {
	mgr *service.AlertManager
}

func (a *recoveryAlertsAdapter) GetActiveAlertCount(sandboxID string) (total, critical, warning int) {
	for _, al := range a.mgr.ListAlerts(service.AlertStatus("")) {
		if al.SandboxID != sandboxID || !isActiveAlert(al.Status) {
			continue
		}
		total++
		switch al.AlertSeverity {
		case service.AlertSeverityCritical:
			critical++
		case service.AlertSeverityWarning:
			warning++
		}
	}
	return total, critical, warning
}

// recoveryLogsAdapter 把 logquery.Service 适配到 service.RecoveryLogProvider（无 ctx）。
type recoveryLogsAdapter struct {
	svc *logquery.Service
}

func (a *recoveryLogsAdapter) CountErrors(sandboxID string, sinceMinutes int) (int, error) {
	end := time.Now()
	start := end.Add(-time.Duration(sinceMinutes) * time.Minute)
	return a.svc.Count(context.Background(), sandboxID, "", logquery.LevelError, start, end)
}

var (
	_ service.RecoveryMetricsProvider = (*recoveryMetricsAdapter)(nil)
	_ service.RecoveryAlertProvider   = (*recoveryAlertsAdapter)(nil)
	_ service.RecoveryLogProvider     = (*recoveryLogsAdapter)(nil)
)

// ========== 弃班处理适配（卡顿扫描 R3 → 会话终止 + 沙箱回收 + 未完成复盘） ==========

// abandonExecTimeout 弃班回调的执行上限。钩子在扫描循环内同步执行，必须有硬超时：
// PG/沙箱侧变慢时不能把整轮卡顿扫描拖死。
const abandonExecTimeout = 10 * time.Second

// abandonAdapter 把会话服务 + 复盘触发端口适配到 StallMonitor 的弃班钩子。
//
// 返回 error 会让扫描器撤销该会话的去重标记并在下一轮重试；只有"确实处理失败"才返回，
// 用户在同一瞬间自己结束了班次（Abandon 未命中）属于正常竞态，按成功吞掉。
type abandonAdapter struct {
	sessions *service.SessionService
	reviews  service.ReviewTriggerWriter // nil：review_service 未配置时只终止班次、不触发复盘
}

func (a *abandonAdapter) OnAbandon(
	ctx context.Context, info model.StallSessionInfo, st model.StallStatus,
) error {
	cctx, cancel := context.WithTimeout(ctx, abandonExecTimeout)
	defer cancel()

	ok, err := a.sessions.Abandon(cctx, info.ID)
	if err != nil {
		return err
	}
	if !ok {
		log.Printf("弃班处理: 会话 %d 已进入终态，跳过（空闲 %ds）", info.ID, st.Evidence.IdleSeconds)
		return nil
	}
	log.Printf("弃班处理: 会话 %d（用户 %d）连续空闲 %ds，班次按未完成结束并回收沙箱",
		info.ID, info.UserID, st.Evidence.IdleSeconds)

	if a.reviews == nil {
		return nil
	}
	// 复盘触发失败不重试：班次已置终态、沙箱已回收，重跑会重复投生成任务。
	// 用户仍可在复盘页手动重试。
	if err := a.reviews.TriggerGeneration(cctx, info.ID, info.UserID, info.ProjectID); err != nil {
		log.Printf("弃班处理: 会话 %d 触发未完成复盘失败: %v", info.ID, err)
	}
	return nil
}
