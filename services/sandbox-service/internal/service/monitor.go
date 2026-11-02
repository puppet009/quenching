package service

import (
	"context"
	"fmt"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
)

// AnomalyAlertCallback 异常告警回调（接入飞书/钉钉/监控系统）。
type AnomalyAlertCallback func(ctx context.Context, anomaly *model.SandboxAnomaly)

// MonitorConfig 监控阈值配置。
type MonitorConfig struct {
	// CPU 告警阈值（百分比）。
	CPUWarningPercent float64 // 默认 70%
	CPUErrorPercent   float64 // 默认 90%

	// 内存告警阈值（百分比）。
	MemoryWarningPercent float64 // 默认 75%
	MemoryErrorPercent   float64 // 默认 90%

	// 长时间运行告警（超过该时长触发）。
	LongRunningWarning time.Duration // 默认 90 分钟
	// 长时间运行告警阈值（默认 110 分钟）：班次制下持续有活动就一直在租期内，
	// 绝对上限 4 小时之前不会自动回收，这条告警是"跑得久得反常"的人工复核信号，不是回收条件。
	LongRunningError time.Duration

	// 状态卡住告警（处于 creating/deploying 超过该时长）。
	StuckStatusDuration time.Duration // 默认 10 分钟

	// 异常 Pod 数量阈值。
	AbnormalPodWarning int // 默认 1
	AbnormalPodError   int // 默认 2

	// 采集间隔（仅供参考，实际采集由定时器控制）。
	ScrapeInterval time.Duration
}

// DefaultMonitorConfig 默认监控配置。
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		CPUWarningPercent:    70,
		CPUErrorPercent:      90,
		MemoryWarningPercent: 75,
		MemoryErrorPercent:   90,
		LongRunningWarning:   90 * time.Minute,
		LongRunningError:     110 * time.Minute,
		StuckStatusDuration:  10 * time.Minute,
		AbnormalPodWarning:   1,
		AbnormalPodError:     2,
		ScrapeInterval:       30 * time.Second,
	}
}

// MonitorService 沙箱监控服务。
//
// 职责：
//   - 资源指标采集与存储
//   - 异常检测（CPU/内存/长时间运行/Pod异常/状态卡住）
//   - 异常告警（回调机制）
//   - 异常记录管理
type MonitorService struct {
	store   MonitorStore
	cfg     MonitorConfig
	alertCb AnomalyAlertCallback
}

// NewMonitorService 创建监控服务。
func NewMonitorService(store MonitorStore, cfg MonitorConfig) *MonitorService {
	return &MonitorService{
		store: store,
		cfg:   cfg,
	}
}

// SetAlertCallback 设置异常告警回调。
func (s *MonitorService) SetAlertCallback(cb AnomalyAlertCallback) {
	s.alertCb = cb
}

// ——— 沙箱列表 & 详情 ———

// ListSandboxes 分页查询沙箱列表。
func (s *MonitorService) ListSandboxes(ctx context.Context, q model.SandboxQuery) (*model.SandboxPage, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 10
	}
	return s.store.List(ctx, q)
}

// GetSandboxDetail 获取沙箱详情（含运行时长、剩余时间、最新指标）。
func (s *MonitorService) GetSandboxDetail(ctx context.Context, sessionID int64) (*model.SandboxDetail, error) {
	sb, err := s.store.GetBySessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sb == nil {
		return nil, ErrSandboxNotFound.Unwrap()
	}

	now := time.Now()
	detail := &model.SandboxDetail{
		Sandbox:          *sb,
		RuntimeSeconds:   sb.ComputeRuntime(now),
		RemainingSeconds: sb.ComputeRemaining(now),
	}

	// 附加最新指标
	metrics, err := s.store.GetLatestMetrics(ctx, sessionID)
	if err == nil && metrics != nil {
		detail.Metrics = metrics
	}

	return detail, nil
}

// GetSandboxStats 获取沙箱统计概览（总数/各状态数/活跃数/未解决异常数）。
func (s *MonitorService) GetSandboxStats(ctx context.Context) (map[string]interface{}, error) {
	byStatus, err := s.store.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	active, err := s.store.CountActive(ctx)
	if err != nil {
		return nil, err
	}
	anomalies, err := s.store.CountUnresolvedAnomalies(ctx)
	if err != nil {
		return nil, err
	}

	total := 0
	for _, count := range byStatus {
		total += count
	}

	return map[string]interface{}{
		"total":                total,
		"active":               active,
		"by_status":            byStatus,
		"unresolved_anomalies": anomalies,
	}, nil
}

// ——— 资源监控 ———

// ReportMetrics 上报资源指标（由采集器调用）。
// 同时进行异常检测，触发告警。
func (s *MonitorService) ReportMetrics(ctx context.Context, metrics *model.ResourceMetrics) error {
	if metrics == nil {
		return fmt.Errorf("指标不能为空")
	}

	// 保存指标
	if err := s.store.SaveMetrics(ctx, metrics); err != nil {
		return err
	}

	// 异常检测
	anomalies := s.detectAnomalies(metrics)
	for _, a := range anomalies {
		if err := s.store.RecordAnomaly(ctx, a); err != nil {
			continue // 记录失败不影响主流程
		}
		// 触发告警回调
		if s.alertCb != nil {
			go s.alertCb(ctx, a)
		}
	}

	return nil
}

// GetLatestMetrics 获取沙箱最新资源指标。
func (s *MonitorService) GetLatestMetrics(ctx context.Context, sessionID int64) (*model.ResourceMetrics, error) {
	return s.store.GetLatestMetrics(ctx, sessionID)
}

// GetMetricsHistory 获取沙箱指标历史（最近 N 条）。
func (s *MonitorService) GetMetricsHistory(ctx context.Context, sessionID int64, limit int) ([]model.ResourceMetrics, error) {
	if limit <= 0 {
		limit = 30 // 默认 30 条
	}
	if limit > 100 {
		limit = 100 // 最多 100 条
	}
	return s.store.ListMetrics(ctx, sessionID, limit)
}

// ——— 异常检测 ———

// detectAnomalies 根据资源指标检测异常。
// 返回检测到的异常列表（可能多个）。
func (s *MonitorService) detectAnomalies(metrics *model.ResourceMetrics) []*model.SandboxAnomaly {
	var anomalies []*model.SandboxAnomaly
	now := time.Now()

	// CPU 过高检测
	if metrics.CPUPercent >= s.cfg.CPUErrorPercent {
		anomalies = append(anomalies, &model.SandboxAnomaly{
			SessionID:   metrics.SessionID,
			Namespace:   metrics.Namespace,
			AnomalyType: model.AnomalyHighCPU,
			Level:       model.AnomalyError,
			Message:     fmt.Sprintf("CPU 使用率 %.1f%%，超过严重阈值 %.1f%%", metrics.CPUPercent, s.cfg.CPUErrorPercent),
			Snapshot:    metrics,
			DetectedAt:  now,
		})
	} else if metrics.CPUPercent >= s.cfg.CPUWarningPercent {
		anomalies = append(anomalies, &model.SandboxAnomaly{
			SessionID:   metrics.SessionID,
			Namespace:   metrics.Namespace,
			AnomalyType: model.AnomalyHighCPU,
			Level:       model.AnomalyWarning,
			Message:     fmt.Sprintf("CPU 使用率 %.1f%%，接近阈值 %.1f%%", metrics.CPUPercent, s.cfg.CPUWarningPercent),
			Snapshot:    metrics,
			DetectedAt:  now,
		})
	}

	// 内存过高检测
	if metrics.MemoryPercent >= s.cfg.MemoryErrorPercent {
		anomalies = append(anomalies, &model.SandboxAnomaly{
			SessionID:   metrics.SessionID,
			Namespace:   metrics.Namespace,
			AnomalyType: model.AnomalyHighMemory,
			Level:       model.AnomalyError,
			Message:     fmt.Sprintf("内存使用率 %.1f%%，超过严重阈值 %.1f%%", metrics.MemoryPercent, s.cfg.MemoryErrorPercent),
			Snapshot:    metrics,
			DetectedAt:  now,
		})
	} else if metrics.MemoryPercent >= s.cfg.MemoryWarningPercent {
		anomalies = append(anomalies, &model.SandboxAnomaly{
			SessionID:   metrics.SessionID,
			Namespace:   metrics.Namespace,
			AnomalyType: model.AnomalyHighMemory,
			Level:       model.AnomalyWarning,
			Message:     fmt.Sprintf("内存使用率 %.1f%%，接近阈值 %.1f%%", metrics.MemoryPercent, s.cfg.MemoryWarningPercent),
			Snapshot:    metrics,
			DetectedAt:  now,
		})
	}

	// Pod 异常检测
	if metrics.PodAbnormal >= s.cfg.AbnormalPodError {
		anomalies = append(anomalies, &model.SandboxAnomaly{
			SessionID:   metrics.SessionID,
			Namespace:   metrics.Namespace,
			AnomalyType: model.AnomalyFailedPod,
			Level:       model.AnomalyError,
			Message:     fmt.Sprintf("异常 Pod 数 %d，超过阈值 %d", metrics.PodAbnormal, s.cfg.AbnormalPodError),
			Snapshot:    metrics,
			DetectedAt:  now,
		})
	} else if metrics.PodAbnormal >= s.cfg.AbnormalPodWarning {
		anomalies = append(anomalies, &model.SandboxAnomaly{
			SessionID:   metrics.SessionID,
			Namespace:   metrics.Namespace,
			AnomalyType: model.AnomalyFailedPod,
			Level:       model.AnomalyWarning,
			Message:     fmt.Sprintf("异常 Pod 数 %d，请注意", metrics.PodAbnormal),
			Snapshot:    metrics,
			DetectedAt:  now,
		})
	}

	return anomalies
}

// DetectStuckStatuses 检测状态卡住的沙箱（供定时任务调用）。
func (s *MonitorService) DetectStuckStatuses(ctx context.Context) ([]*model.SandboxAnomaly, error) {
	active, err := s.store.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	var anomalies []*model.SandboxAnomaly
	now := time.Now()
	stuckDuration := s.cfg.StuckStatusDuration

	for _, sb := range active {
		// 只有 creating 和 deploying 状态才可能"卡住"
		if sb.Status != model.StatusCreating && sb.Status != model.StatusDeploying {
			continue
		}

		// 处于该状态的时长
		duration := now.Sub(sb.CreatedAt)
		if duration >= stuckDuration {
			anomaly := &model.SandboxAnomaly{
				SessionID:   sb.SessionID,
				Namespace:   sb.Namespace,
				UserID:      sb.UserID,
				AnomalyType: model.AnomalyStuckStatus,
				Level:       model.AnomalyError,
				Message:     fmt.Sprintf("沙箱处于 %s 状态超过 %s，可能已卡住", sb.Status, stuckDuration),
				DetectedAt:  now,
			}
			anomalies = append(anomalies, anomaly)

			// 记录异常
			if err := s.store.RecordAnomaly(ctx, anomaly); err != nil {
				continue
			}
			if s.alertCb != nil {
				go s.alertCb(ctx, anomaly)
			}
		}
	}

	return anomalies, nil
}

// DetectLongRunning 检测长时间运行的沙箱（供定时任务调用）。
func (s *MonitorService) DetectLongRunning(ctx context.Context) ([]*model.SandboxAnomaly, error) {
	active, err := s.store.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	var anomalies []*model.SandboxAnomaly
	now := time.Now()

	for _, sb := range active {
		runtime := now.Sub(sb.CreatedAt)

		var level model.AnomalyLevel
		var threshold time.Duration

		if runtime >= s.cfg.LongRunningError {
			level = model.AnomalyError
			threshold = s.cfg.LongRunningError
		} else if runtime >= s.cfg.LongRunningWarning {
			level = model.AnomalyWarning
			threshold = s.cfg.LongRunningWarning
		} else {
			continue
		}

		anomaly := &model.SandboxAnomaly{
			SessionID:   sb.SessionID,
			Namespace:   sb.Namespace,
			UserID:      sb.UserID,
			AnomalyType: model.AnomalyLongRunning,
			Level:       level,
			Message:     fmt.Sprintf("沙箱已运行 %.0f 分钟，超过 %s 阈值", runtime.Minutes(), threshold),
			DetectedAt:  now,
		}
		anomalies = append(anomalies, anomaly)

		if err := s.store.RecordAnomaly(ctx, anomaly); err != nil {
			continue
		}
		if s.alertCb != nil {
			go s.alertCb(ctx, anomaly)
		}
	}

	return anomalies, nil
}

// ——— 异常管理 ———

// ListAnomalies 分页查询异常列表。
func (s *MonitorService) ListAnomalies(ctx context.Context, q model.AnomalyQuery) (*model.AnomalyPage, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 10
	}
	return s.store.ListAnomalies(ctx, q)
}

// ResolveAnomaly 标记异常已解决。
func (s *MonitorService) ResolveAnomaly(ctx context.Context, anomalyID int64) error {
	if anomalyID <= 0 {
		return fmt.Errorf("异常ID非法")
	}
	return s.store.ResolveAnomaly(ctx, anomalyID)
}

// ——— 手动释放 ———

// ReleaseSandbox 手动释放沙箱。
// 标记为 releasing 状态，实际释放由调谐器异步执行。
func (s *MonitorService) ReleaseSandbox(ctx context.Context, sessionID int64, reason string) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID非法")
	}

	// 检查沙箱是否存在
	sb, err := s.store.GetBySessionID(ctx, sessionID)
	if err != nil {
		return err
	}
	if sb == nil {
		return ErrSandboxNotFound.Unwrap()
	}

	// 检查状态
	if sb.Status == model.StatusReleased {
		return fmt.Errorf("沙箱已释放")
	}
	if sb.Status == model.StatusReleasing {
		return fmt.Errorf("沙箱正在释放中")
	}

	return s.store.Release(ctx, sessionID)
}
