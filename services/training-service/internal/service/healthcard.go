// Package service 中服务健康卡片（T1-20）。
//
// 整合指标数据（T1-15）和告警数据（T1-23），
// 提供服务健康卡片视图，支持实时推送。
package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ========== 健康状态 ==========

// HealthStatus 健康状态。
type HealthStatus string

const (
	StatusHealthy  HealthStatus = "healthy"  // 健康
	StatusWarning  HealthStatus = "warning"  // 警告（有 warning 告警）
	StatusCritical HealthStatus = "critical" // 严重（有 critical 告警或指标超标）
	StatusUnknown  HealthStatus = "unknown"  // 未知（数据缺失）
)

// ========== 数据结构 ==========

// ServiceCard 单个服务的健康卡片。
type ServiceCard struct {
	Service       string       `json:"service"`          // 服务名
	Status        HealthStatus `json:"status"`           // 健康状态
	CPUUsage      float64      `json:"cpu_usage"`        // CPU 使用率 (%)
	MemoryUsage   float64      `json:"memory_usage"`     // 内存使用率 (%)
	QPS           float64      `json:"qps"`              // QPS (次/秒)
	ErrorRate     float64      `json:"error_rate"`       // 错误率 (%)
	P99Latency    float64      `json:"p99_latency"`      // P99 延迟 (秒)
	AlertCount    int          `json:"alert_count"`      // 活跃告警数
	CriticalCount int          `json:"critical_count"`   // critical 告警数
	WarningCount  int          `json:"warning_count"`    // warning 告警数
	Reason        string       `json:"reason,omitempty"` // 状态原因
	UpdatedAt     int64        `json:"updated_at"`       // 更新时间（毫秒）
}

// DashboardOverview 健康概览（整个沙箱的健康状态）。
type DashboardOverview struct {
	SandboxID     string        `json:"sandbox_id"`     // 沙箱ID
	TotalServices int           `json:"total_services"` // 服务总数
	HealthyCount  int           `json:"healthy_count"`  // 健康服务数
	WarningCount  int           `json:"warning_count"`  // 警告服务数
	CriticalCount int           `json:"critical_count"` // 严重服务数
	UnknownCount  int           `json:"unknown_count"`  // 未知服务数
	TotalAlerts   int           `json:"total_alerts"`   // 总告警数
	Services      []ServiceCard `json:"services"`       // 各服务卡片
	UpdatedAt     int64         `json:"updated_at"`     // 更新时间
}

// AlertSummary 告警摘要。
type AlertSummary struct {
	Total    int `json:"total"`    // 总告警数
	Critical int `json:"critical"` // critical 级别
	Warning  int `json:"warning"`  // warning 级别
	Info     int `json:"info"`     // info 级别
}

// ========== 依赖接口 ==========

// HealthCardMetricsProvider 指标数据提供者。
type HealthCardMetricsProvider interface {
	// CheckServiceHealth 检查服务健康状态。
	CheckServiceHealth(ctx context.Context, service string) (ServiceHealth, error)

	// GetAllMetrics 获取所有核心指标快照。
	GetAllMetrics(ctx context.Context, service string) (AllMetricsSnapshot, error)
}

// ServiceHealth 服务健康状态（简化版，从 metrics 包映射）。
type ServiceHealth struct {
	Service     string  `json:"service"`
	Healthy     bool    `json:"healthy"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	ErrorRate   float64 `json:"error_rate"`
	Reason      string  `json:"reason,omitempty"`
}

// AllMetricsSnapshot 所有指标快照（简化版）。
type AllMetricsSnapshot struct {
	CPUUsage    MetricValue `json:"cpu_usage"`
	MemoryUsage MetricValue `json:"memory_usage"`
	QPS         MetricValue `json:"qps"`
	ErrorRate   MetricValue `json:"error_rate"`
	P99Latency  MetricValue `json:"p99_latency"`
	Timestamp   int64       `json:"timestamp"`
}

// MetricValue 指标值。
type MetricValue struct {
	Value       float64 `json:"value"`
	Unit        string  `json:"unit"`
	DisplayName string  `json:"display_name"`
}

// HealthCardAlertProvider 告警数据提供者。
type HealthCardAlertProvider interface {
	// ListAlerts 列出指定沙箱的告警（按状态过滤）。
	ListAlerts(sandboxID string, status string) []AlertInfo

	// AlertCountByService 按服务统计告警数。
	AlertCountByService(sandboxID string) map[string]AlertCount
}

// AlertInfo 告警信息（简化版）。
type AlertInfo struct {
	ID       string `json:"id"`
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Severity string `json:"severity"`
	Service  string `json:"service"`
	Message  string `json:"message"`
	Status   string `json:"status"`
}

// AlertCount 告警计数。
type AlertCount struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}

// EventPublisher 事件发布者。
type EventPublisher interface {
	PublishToChannel(ctx context.Context, channel string, eventType string, data interface{}) error
}

// ========== 服务列表配置 ==========

// 默认服务列表（电商模板的 3 个业务服务）。
var defaultServices = []string{
	"user-service",
	"product-service",
	"order-service",
}

// ========== 健康卡片服务 ==========

// HealthCardService 健康卡片服务。
type HealthCardService struct {
	metrics   HealthCardMetricsProvider
	alerts    HealthCardAlertProvider
	publisher EventPublisher
	services  []string // 监控的服务列表

	// 缓存（避免每次都全量查询）
	cache    *DashboardOverview
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

// NewHealthCardService 创建健康卡片服务。
func NewHealthCardService(metrics HealthCardMetricsProvider, alerts HealthCardAlertProvider, publisher EventPublisher) *HealthCardService {
	return &HealthCardService{
		metrics:   metrics,
		alerts:    alerts,
		publisher: publisher,
		services:  defaultServices,
		cacheTTL:  10 * time.Second,
	}
}

// SetServices 设置监控的服务列表。
func (s *HealthCardService) SetServices(services []string) {
	s.services = services
}

// GetOverview 获取健康概览。
//
// 优先使用缓存，缓存过期则重新计算。
func (s *HealthCardService) GetOverview(ctx context.Context, sandboxID string) (*DashboardOverview, error) {
	// 检查缓存
	s.cacheMu.RLock()
	if s.cache != nil && s.cache.SandboxID == sandboxID {
		elapsed := time.Since(time.UnixMilli(s.cache.UpdatedAt))
		if elapsed < s.cacheTTL {
			result := *s.cache // 返回副本
			s.cacheMu.RUnlock()
			return &result, nil
		}
	}
	s.cacheMu.RUnlock()

	// 重新计算
	overview, err := s.buildOverview(ctx, sandboxID)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	s.cacheMu.Lock()
	s.cache = overview
	s.cacheMu.Unlock()

	return overview, nil
}

// GetServiceCard 获取单个服务的健康卡片。
func (s *HealthCardService) GetServiceCard(ctx context.Context, sandboxID, service string) (*ServiceCard, error) {
	// 先拿概览（走缓存）
	overview, err := s.GetOverview(ctx, sandboxID)
	if err != nil {
		return nil, err
	}

	for _, card := range overview.Services {
		if card.Service == service {
			result := card
			return &result, nil
		}
	}

	return nil, fmt.Errorf("服务不存在: %s", service)
}

// Refresh 强制刷新缓存。
func (s *HealthCardService) Refresh(ctx context.Context, sandboxID string) (*DashboardOverview, error) {
	overview, err := s.buildOverview(ctx, sandboxID)
	if err != nil {
		return nil, err
	}

	s.cacheMu.Lock()
	s.cache = overview
	s.cacheMu.Unlock()

	return overview, nil
}

// buildOverview 构建健康概览（从指标和告警数据聚合）。
func (s *HealthCardService) buildOverview(ctx context.Context, sandboxID string) (*DashboardOverview, error) {
	overview := &DashboardOverview{
		SandboxID: sandboxID,
		Services:  make([]ServiceCard, 0, len(s.services)),
		UpdatedAt: time.Now().UnixMilli(),
	}

	// 获取告警数据（按服务统计）
	alertMap := s.alerts.AlertCountByService(sandboxID)

	// 并发查询每个服务的指标
	type result struct {
		card ServiceCard
		err  error
	}
	ch := make(chan result, len(s.services))

	for _, svc := range s.services {
		go func(service string) {
			card, err := s.buildServiceCard(ctx, sandboxID, service, alertMap[service])
			ch <- result{card: card, err: err}
		}(svc)
	}

	for i := 0; i < len(s.services); i++ {
		r := <-ch
		if r.err != nil {
			// 单个服务失败不影响整体，标记为 unknown
			r.card.Status = StatusUnknown
			r.card.Service = s.services[i]
			r.card.Reason = "数据获取失败"
		}
		overview.Services = append(overview.Services, r.card)
		overview.TotalServices++

		switch r.card.Status {
		case StatusHealthy:
			overview.HealthyCount++
		case StatusWarning:
			overview.WarningCount++
		case StatusCritical:
			overview.CriticalCount++
		case StatusUnknown:
			overview.UnknownCount++
		}

		overview.TotalAlerts += r.card.AlertCount
	}

	// 按严重程度排序（critical > warning > healthy > unknown）
	sortServicesBySeverity(overview.Services)

	return overview, nil
}

// buildServiceCard 构建单个服务的健康卡片。
func (s *HealthCardService) buildServiceCard(ctx context.Context, sandboxID, service string, alerts AlertCount) (ServiceCard, error) {
	card := ServiceCard{
		Service:       service,
		AlertCount:    alerts.Total,
		CriticalCount: alerts.Critical,
		WarningCount:  alerts.Warning,
		UpdatedAt:     time.Now().UnixMilli(),
	}

	// 获取指标数据
	health, err := s.metrics.CheckServiceHealth(ctx, service)
	if err != nil {
		return card, err
	}

	card.CPUUsage = health.CPUUsage
	card.MemoryUsage = health.MemoryUsage
	card.ErrorRate = health.ErrorRate

	// 获取完整指标快照（包含 QPS 和 P99）
	snapshot, err := s.metrics.GetAllMetrics(ctx, service)
	if err == nil {
		card.QPS = snapshot.QPS.Value
		card.P99Latency = snapshot.P99Latency.Value
	}

	// 判定健康状态
	card.Status = s.determineStatus(health, alerts)

	if card.Status == StatusCritical && health.Reason != "" {
		card.Reason = health.Reason
	} else if card.Status == StatusCritical && alerts.Critical > 0 {
		card.Reason = fmt.Sprintf("%d 个严重告警", alerts.Critical)
	} else if card.Status == StatusWarning && alerts.Warning > 0 {
		card.Reason = fmt.Sprintf("%d 个警告", alerts.Warning)
	}

	return card, nil
}

// determineStatus 根据健康检查和告警数据判定状态。
//
// 判定优先级：
//  1. 有 critical 告警 → critical
//  2. 指标不健康 → critical
//  3. 有 warning 告警 → warning
//  4. 都正常 → healthy
func (s *HealthCardService) determineStatus(health ServiceHealth, alerts AlertCount) HealthStatus {
	if alerts.Critical > 0 {
		return StatusCritical
	}
	if !health.Healthy {
		return StatusCritical
	}
	if alerts.Warning > 0 {
		return StatusWarning
	}
	return StatusHealthy
}

// sortServicesBySeverity 按严重程度排序服务卡片。
//
// 顺序：critical → warning → unknown → healthy
func sortServicesBySeverity(services []ServiceCard) {
	n := len(services)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if severityRank(services[j].Status) < severityRank(services[j+1].Status) {
				services[j], services[j+1] = services[j+1], services[j]
			}
		}
	}
}

// severityRank 状态严重程度排名（数字越大越严重）。
func severityRank(status HealthStatus) int {
	switch status {
	case StatusCritical:
		return 3
	case StatusWarning:
		return 2
	case StatusUnknown:
		return 1
	case StatusHealthy:
		return 0
	default:
		return 0
	}
}

// ========== 实时推送 ==========

// StartPush 启动定时推送健康状态。
//
// 每隔 interval 时间推送一次健康概览到 sandbox 频道。
func (s *HealthCardService) StartPush(ctx context.Context, sandboxID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即推送一次
	s.pushHealthStatus(ctx, sandboxID)

	for {
		select {
		case <-ticker.C:
			s.pushHealthStatus(ctx, sandboxID)
		case <-ctx.Done():
			return
		}
	}
}

// pushHealthStatus 推送健康状态到事件总线。
func (s *HealthCardService) pushHealthStatus(ctx context.Context, sandboxID string) {
	if s.publisher == nil {
		return
	}

	overview, err := s.Refresh(ctx, sandboxID)
	if err != nil {
		return
	}

	channel := "sandbox:" + sandboxID
	s.publisher.PublishToChannel(ctx, channel, "health_status", overview)
}
