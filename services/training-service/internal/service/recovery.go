// Package service 中故障恢复验证（T1-29）。
//
// 多维度验证沙箱是否从故障中恢复：
//   - 指标检查（CPU/内存/错误率/QPS）
//   - 告警检查（无活跃告警）
//   - 日志检查（无错误激增）
//   - 服务健康检查（所有服务健康）
//   - 接口探活（关键接口正常返回）
package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ========== 检查状态 ==========

// CheckStatus 检查项状态。
type CheckStatus string

const (
	StatusPassed  CheckStatus = "passed"  // 通过（已恢复）
	StatusFailed  CheckStatus = "failed"  // 未通过（未恢复）
	StatusSkipped CheckStatus = "skipped" // 跳过（不适用）
	StatusPending CheckStatus = "pending" // 待检查
)

// ========== 检查项类别 ==========

// CheckCategory 检查项类别。
type CheckCategory string

const (
	CategoryMetrics  CheckCategory = "metrics"  // 指标检查
	CategoryAlerts   CheckCategory = "alerts"   // 告警检查
	CategoryLogs     CheckCategory = "logs"     // 日志检查
	CategoryHealth   CheckCategory = "health"   // 健康检查
	CategoryProbe    CheckCategory = "probe"    // 接口探活
	CategoryResource CheckCategory = "resource" // 资源检查
)

// ========== 检查项定义 ==========

// CheckItem 单个恢复检查项。
type CheckItem struct {
	ID          string        `json:"id"`          // 检查项ID
	Name        string        `json:"name"`        // 检查项名称
	Category    CheckCategory `json:"category"`    // 类别
	Description string        `json:"description"` // 描述
	Weight      float64       `json:"weight"`      // 权重（0-1，总分 100）
	Enabled     bool          `json:"enabled"`     // 是否启用
	Status      CheckStatus   `json:"status"`      // 检查状态
	Message     string        `json:"message"`     // 检查结果描述
	DurationMs  int64         `json:"duration_ms"` // 检查耗时（毫秒）
	CheckedAt   int64         `json:"checked_at"`  // 检查时间
}

// ========== 恢复验证结果 ==========

// VerificationResult 恢复验证结果。
type VerificationResult struct {
	SessionID     string      `json:"session_id"`     // 训练会话ID
	SandboxID     string      `json:"sandbox_id"`     // 沙箱ID
	OverallStatus CheckStatus `json:"overall_status"` // 总体状态
	Score         float64     `json:"score"`          // 恢复得分（0-100）
	TotalChecks   int         `json:"total_checks"`   // 总检查项数
	PassedCount   int         `json:"passed_count"`   // 通过数
	FailedCount   int         `json:"failed_count"`   // 失败数
	SkippedCount  int         `json:"skipped_count"`  // 跳过数
	Checks        []CheckItem `json:"checks"`         // 各检查项详情
	StartTime     int64       `json:"start_time"`     // 开始时间
	EndTime       int64       `json:"end_time"`       // 结束时间
	DurationMs    int64       `json:"duration_ms"`    // 总耗时
}

// ========== 检查器接口 ==========

// Checker 检查器接口。
//
// 每个检查类别有一个 Checker 实现。
type Checker interface {
	// Category 返回检查类别。
	Category() CheckCategory

	// Check 执行检查，返回检查项列表。
	Check(ctx context.Context, sandboxID string) ([]CheckItem, error)
}

// ========== 依赖接口 ==========

// RecoveryMetricsProvider 指标数据提供者。
type RecoveryMetricsProvider interface {
	// GetServiceHealth 获取服务健康状态。
	GetServiceHealth(service string) (ServiceHealth, error)

	// GetMetricValue 获取指标当前值。
	GetMetricValue(service, metric string) (float64, error)
}

// RecoveryAlertProvider 告警数据提供者。
type RecoveryAlertProvider interface {
	// GetActiveAlertCount 获取活跃告警数（firing + acknowledged）。
	GetActiveAlertCount(sandboxID string) (total, critical, warning int)
}

// RecoveryLogProvider 日志数据提供者。
type RecoveryLogProvider interface {
	// CountErrors 统计最近 N 分钟内的错误日志数。
	CountErrors(sandboxID string, sinceMinutes int) (int, error)
}

// ProbeProvider 接口探活提供者。
type ProbeProvider interface {
	// CheckEndpoint 检查端点是否正常返回 200。
	CheckEndpoint(url string, timeout time.Duration) (bool, int, error)
}

// ========== 恢复验证服务 ==========

// RecoveryService 恢复验证服务。
type RecoveryService struct {
	checkers map[CheckCategory]Checker
	// 缓存最近一次验证结果
	lastResult map[string]*VerificationResult // sessionID -> result
	mu         sync.RWMutex
}

// NewRecoveryService 创建恢复验证服务。
func NewRecoveryService() *RecoveryService {
	return &RecoveryService{
		checkers:   make(map[CheckCategory]Checker),
		lastResult: make(map[string]*VerificationResult),
	}
}

// RegisterChecker 注册检查器。
func (s *RecoveryService) RegisterChecker(checker Checker) {
	s.checkers[checker.Category()] = checker
}

// Verify 执行一次完整的恢复验证。
func (s *RecoveryService) Verify(ctx context.Context, sandboxID, sessionID string) (*VerificationResult, error) {
	start := time.Now()

	result := &VerificationResult{
		SessionID:     sessionID,
		SandboxID:     sandboxID,
		OverallStatus: StatusPending,
		Checks:        make([]CheckItem, 0),
		StartTime:     start.UnixMilli(),
	}

	// 并发执行所有检查器
	type r struct {
		category CheckCategory
		items    []CheckItem
		err      error
	}
	ch := make(chan r, len(s.checkers))

	for _, checker := range s.checkers {
		go func(c Checker) {
			items, err := c.Check(ctx, sandboxID)
			ch <- r{category: c.Category(), items: items, err: err}
		}(checker)
	}

	// 收集结果
	for i := 0; i < len(s.checkers); i++ {
		res := <-ch
		if res.err != nil {
			// 单个检查器失败，标记为 failed
			failedItem := CheckItem{
				ID:       string(res.category) + "_error",
				Name:     string(res.category) + " 检查失败",
				Category: res.category,
				Status:   StatusFailed,
				Message:  res.err.Error(),
			}
			result.Checks = append(result.Checks, failedItem)
			continue
		}
		result.Checks = append(result.Checks, res.items...)
	}

	result.TotalChecks = len(result.Checks)

	// 计算得分和状态
	s.calculateResult(result)

	end := time.Now()
	result.EndTime = end.UnixMilli()
	result.DurationMs = end.Sub(start).Milliseconds()

	// 缓存结果
	s.mu.Lock()
	s.lastResult[sessionID] = result
	s.mu.Unlock()

	return result, nil
}

// GetLastResult 获取最近一次验证结果。
func (s *RecoveryService) GetLastResult(sessionID string) (*VerificationResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result, ok := s.lastResult[sessionID]
	return result, ok
}

// calculateResult 计算总体结果和得分。
func (s *RecoveryService) calculateResult(result *VerificationResult) {
	var totalWeight float64
	var passedWeight float64

	for i := range result.Checks {
		item := &result.Checks[i]
		if !item.Enabled {
			item.Status = StatusSkipped
		}

		switch item.Status {
		case StatusPassed:
			result.PassedCount++
			passedWeight += item.Weight
			totalWeight += item.Weight
		case StatusFailed:
			result.FailedCount++
			totalWeight += item.Weight
		case StatusSkipped:
			result.SkippedCount++
			// 跳过的不计入权重
		}
	}

	// 计算得分（按权重加权）
	if totalWeight > 0 {
		result.Score = (passedWeight / totalWeight) * 100
	} else {
		result.Score = 100 // 没有有效检查项，默认满分
	}

	// 判定总体状态
	if result.FailedCount == 0 {
		result.OverallStatus = StatusPassed
	} else {
		result.OverallStatus = StatusFailed
	}
}

// ========== 内置检查器 ==========

// --- 指标检查器 ---

// MetricsChecker 指标检查器。
type MetricsChecker struct {
	provider RecoveryMetricsProvider
	services []string
}

// NewMetricsChecker 创建指标检查器。
func NewMetricsChecker(provider RecoveryMetricsProvider, services []string) *MetricsChecker {
	return &MetricsChecker{provider: provider, services: services}
}

func (c *MetricsChecker) Category() CheckCategory { return CategoryMetrics }

func (c *MetricsChecker) Check(ctx context.Context, sandboxID string) ([]CheckItem, error) {
	var items []CheckItem

	for _, svc := range c.services {
		health, err := c.provider.GetServiceHealth(svc)
		if err != nil {
			items = append(items, CheckItem{
				ID:          "metrics_" + svc + "_health",
				Name:        svc + " 指标健康",
				Category:    CategoryMetrics,
				Description: "检查 " + svc + " 的 CPU/内存/错误率是否正常",
				Weight:      10,
				Enabled:     true,
				Status:      StatusFailed,
				Message:     "获取指标失败: " + err.Error(),
				CheckedAt:   time.Now().UnixMilli(),
			})
			continue
		}

		status := StatusPassed
		message := svc + " 指标正常"
		if !health.Healthy {
			status = StatusFailed
			message = health.Reason
		}

		items = append(items, CheckItem{
			ID:          "metrics_" + svc + "_health",
			Name:        svc + " 指标健康",
			Category:    CategoryMetrics,
			Description: "检查 " + svc + " 的 CPU/内存/错误率是否正常",
			Weight:      10,
			Enabled:     true,
			Status:      status,
			Message:     message,
			CheckedAt:   time.Now().UnixMilli(),
		})
	}

	return items, nil
}

// --- 告警检查器 ---

// AlertChecker 告警检查器。
type AlertChecker struct {
	provider RecoveryAlertProvider
}

// NewAlertChecker 创建告警检查器。
func NewAlertChecker(provider RecoveryAlertProvider) *AlertChecker {
	return &AlertChecker{provider: provider}
}

func (c *AlertChecker) Category() CheckCategory { return CategoryAlerts }

func (c *AlertChecker) Check(ctx context.Context, sandboxID string) ([]CheckItem, error) {
	total, critical, warning := c.provider.GetActiveAlertCount(sandboxID)

	// Critical 告警检查
	criticalItem := CheckItem{
		ID:          "alerts_no_critical",
		Name:        "无严重告警",
		Category:    CategoryAlerts,
		Description: "检查是否存在 Critical 级别的活跃告警",
		Weight:      20,
		Enabled:     true,
		CheckedAt:   time.Now().UnixMilli(),
	}
	if critical == 0 {
		criticalItem.Status = StatusPassed
		criticalItem.Message = "无 Critical 告警"
	} else {
		criticalItem.Status = StatusFailed
		criticalItem.Message = fmt.Sprintf("存在 %d 条 Critical 告警", critical)
	}

	// Warning 告警检查
	warningItem := CheckItem{
		ID:          "alerts_no_warning",
		Name:        "无警告告警",
		Category:    CategoryAlerts,
		Description: "检查是否存在 Warning 级别的活跃告警",
		Weight:      10,
		Enabled:     true,
		CheckedAt:   time.Now().UnixMilli(),
	}
	if warning == 0 {
		warningItem.Status = StatusPassed
		warningItem.Message = "无 Warning 告警"
	} else {
		warningItem.Status = StatusFailed
		warningItem.Message = fmt.Sprintf("存在 %d 条 Warning 告警", warning)
	}

	_ = total // 总数暂时不用

	return []CheckItem{criticalItem, warningItem}, nil
}

// --- 日志检查器 ---

// LogChecker 日志检查器。
type LogChecker struct {
	provider       RecoveryLogProvider
	errorThreshold int // 错误阈值（最近 N 分钟内超过此数则未恢复）
	minutes        int // 检查最近几分钟
}

// NewLogChecker 创建日志检查器。
func NewLogChecker(provider RecoveryLogProvider) *LogChecker {
	return &LogChecker{
		provider:       provider,
		errorThreshold: 5,
		minutes:        5,
	}
}

func (c *LogChecker) Category() CheckCategory { return CategoryLogs }

func (c *LogChecker) Check(ctx context.Context, sandboxID string) ([]CheckItem, error) {
	count, err := c.provider.CountErrors(sandboxID, c.minutes)
	if err != nil {
		return []CheckItem{{
			ID:          "logs_error_rate",
			Name:        "错误日志数量",
			Category:    CategoryLogs,
			Description: fmt.Sprintf("检查最近 %d 分钟的错误日志数量", c.minutes),
			Weight:      10,
			Enabled:     true,
			Status:      StatusFailed,
			Message:     "获取日志统计失败: " + err.Error(),
			CheckedAt:   time.Now().UnixMilli(),
		}}, nil
	}

	status := StatusPassed
	message := fmt.Sprintf("最近 %d 分钟错误日志 %d 条，正常", c.minutes, count)
	if count > c.errorThreshold {
		status = StatusFailed
		message = fmt.Sprintf("最近 %d 分钟错误日志 %d 条，超过阈值 %d", c.minutes, count, c.errorThreshold)
	}

	return []CheckItem{{
		ID:          "logs_error_rate",
		Name:        "错误日志数量",
		Category:    CategoryLogs,
		Description: fmt.Sprintf("检查最近 %d 分钟的错误日志数量", c.minutes),
		Weight:      10,
		Enabled:     true,
		Status:      status,
		Message:     message,
		CheckedAt:   time.Now().UnixMilli(),
	}}, nil
}

// --- 健康检查器 ---

// HealthChecker 服务健康检查器。
type HealthChecker struct {
	provider RecoveryMetricsProvider
	services []string
}

// NewHealthChecker 创建健康检查器。
func NewHealthChecker(provider RecoveryMetricsProvider, services []string) *HealthChecker {
	return &HealthChecker{provider: provider, services: services}
}

func (c *HealthChecker) Category() CheckCategory { return CategoryHealth }

func (c *HealthChecker) Check(ctx context.Context, sandboxID string) ([]CheckItem, error) {
	var items []CheckItem

	allHealthy := true
	for _, svc := range c.services {
		health, err := c.provider.GetServiceHealth(svc)
		if err != nil {
			allHealthy = false
			continue
		}
		if !health.Healthy {
			allHealthy = false
		}
	}

	// 整体健康检查
	overallItem := CheckItem{
		ID:          "health_all_services",
		Name:        "所有服务健康",
		Category:    CategoryHealth,
		Description: "检查所有业务服务是否处于健康状态",
		Weight:      15,
		Enabled:     true,
		CheckedAt:   time.Now().UnixMilli(),
	}
	if allHealthy {
		overallItem.Status = StatusPassed
		overallItem.Message = "所有服务健康"
	} else {
		overallItem.Status = StatusFailed
		overallItem.Message = "部分服务不健康"
	}

	items = append(items, overallItem)
	return items, nil
}

// --- 接口探活检查器 ---

// ProbeChecker 接口探活检查器。
type ProbeChecker struct {
	provider  ProbeProvider
	endpoints []ProbeEndpoint
}

// ProbeEndpoint 探活端点配置。
type ProbeEndpoint struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	URL     string        `json:"url"`
	Weight  float64       `json:"weight"`
	Timeout time.Duration `json:"timeout"`
}

// NewProbeChecker 创建接口探活检查器。
func NewProbeChecker(provider ProbeProvider, endpoints []ProbeEndpoint) *ProbeChecker {
	return &ProbeChecker{provider: provider, endpoints: endpoints}
}

func (c *ProbeChecker) Category() CheckCategory { return CategoryProbe }

func (c *ProbeChecker) Check(ctx context.Context, sandboxID string) ([]CheckItem, error) {
	var items []CheckItem

	for _, ep := range c.endpoints {
		ok, statusCode, err := c.provider.CheckEndpoint(ep.URL, ep.Timeout)

		item := CheckItem{
			ID:          "probe_" + ep.ID,
			Name:        ep.Name + " 探活",
			Category:    CategoryProbe,
			Description: "检查 " + ep.URL + " 是否正常返回",
			Weight:      ep.Weight,
			Enabled:     true,
			CheckedAt:   time.Now().UnixMilli(),
		}

		if err != nil {
			item.Status = StatusFailed
			item.Message = "探活失败: " + err.Error()
		} else if ok {
			item.Status = StatusPassed
			item.Message = fmt.Sprintf("正常 (HTTP %d)", statusCode)
		} else {
			item.Status = StatusFailed
			item.Message = fmt.Sprintf("异常 (HTTP %d)", statusCode)
		}

		items = append(items, item)
	}

	return items, nil
}
