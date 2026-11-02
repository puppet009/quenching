// 沙箱默认告警规则注册（T1-23 + 第三批前端事件告警）。
//
// 每个沙箱就绪时由 sandbox-service 通过内部接口触发注册，默认 11 条：
//   - 4 条前端事件型（行为引擎浏览器侧观测，事件驱动 push）
//   - 7 条后端型（T1-23 文档定义：阈值/趋势/日志关键词，定时 pull）
//
// 注册幂等：规则已存在时跳过，重复调用（沙箱重连/重启）不产生重复规则。
package service

import (
	"fmt"
	"time"
)

// DefaultRecoveryWindow 前端事件型默认自动恢复窗口：连续 60 秒无错误即恢复。
const DefaultRecoveryWindow = 60 * time.Second

// RegisterDefaults 为指定沙箱幂等注册全部默认告警规则。
//
// sandboxID 为空时不执行任何操作；返回本次实际新增的规则数。
func RegisterDefaults(mgr *AlertManager, sandboxID string) int {
	if mgr == nil || sandboxID == "" {
		return 0
	}

	added := 0
	for _, rule := range defaultRules(sandboxID) {
		if _, ok := mgr.GetRule(rule.ID); ok {
			continue
		}
		mgr.AddRule(rule)
		added++
	}
	return added
}

// defaultRules 构造单个沙箱的 11 条默认规则。
//
// 规则 ID 带沙箱前缀，保证多沙箱共存且注册幂等。
func defaultRules(sandboxID string) []*AlertRule {
	backendLabels := map[string]string{"source": AlertSourceBackend}
	frontendLabels := map[string]string{"source": AlertSourceFrontend}

	frontendEvent := func(id, name string, severity AlertSeverity, types []string) *AlertRule {
		return &AlertRule{
			ID:             fmt.Sprintf("default-%s-%s", sandboxID, id),
			Name:           name,
			Type:           AlertRuleTypeFrontendEvent,
			AlertSeverity:  severity,
			Enabled:        true,
			SandboxID:      sandboxID,
			EventTypes:     types,
			ForDuration:    0, // 首个错误事件即进入告警
			RecoveryWindow: DefaultRecoveryWindow,
			Labels:         frontendLabels,
		}
	}

	threshold := func(id, name string, severity AlertSeverity, metric string, op AlertOperator, value float64) *AlertRule {
		return &AlertRule{
			ID:            fmt.Sprintf("default-%s-%s", sandboxID, id),
			Name:          name,
			Type:          AlertRuleTypeThreshold,
			AlertSeverity: severity,
			Enabled:       true,
			SandboxID:     sandboxID,
			Metric:        metric,
			AlertOperator: op,
			Threshold:     value,
			ForDuration:   30 * time.Second, // 越过一个评估周期的防抖
			Labels:        backendLabels,
		}
	}

	return []*AlertRule{
		// ========== 前端事件型（4 条） ==========
		frontendEvent("fe-step",
			"页面操作步骤失败",
			AlertSeverityWarning,
			[]string{FrontendSignalStepFailure}),

		frontendEvent("fe-pageerror",
			"页面脚本异常",
			AlertSeverityCritical,
			[]string{FrontendSignalPageError}),

		frontendEvent("fe-http",
			"页面接口异常/请求失败",
			AlertSeverityWarning,
			[]string{FrontendSignalHTTPError, FrontendSignalRequestFailed}),

		frontendEvent("fe-console",
			"浏览器控制台报错",
			AlertSeverityInfo,
			[]string{FrontendSignalConsoleError}),

		// ========== 后端型（T1-23 文档 7 条） ==========
		threshold("be-cpu",
			"CPU 使用率过高",
			AlertSeverityCritical,
			"cpu_usage", AlertOpGreaterThan, 90),

		threshold("be-memory",
			"内存使用率过高",
			AlertSeverityCritical,
			"memory_usage", AlertOpGreaterThan, 90),

		threshold("be-errorrate",
			"错误率过高",
			AlertSeverityWarning,
			"error_rate", AlertOpGreaterThan, 5),

		threshold("be-unavailable",
			"服务不可用",
			AlertSeverityCritical,
			"qps", AlertOpLessThan, 1),

		{
			ID:                  fmt.Sprintf("default-%s-be-memtrend", sandboxID),
			Name:                "内存持续上升",
			Type:                AlertRuleTypeTrend,
			AlertSeverity:       AlertSeverityWarning,
			Enabled:             true,
			SandboxID:           sandboxID,
			Metric:              "memory_usage",
			AlertTrendDirection: AlertTrendUp,
			TrendChangePct:      20,
			TrendDuration:       5 * time.Minute,
			ForDuration:         30 * time.Second,
			Labels:              backendLabels,
		},

		{
			ID:            fmt.Sprintf("default-%s-be-oom", sandboxID),
			Name:          "OOM 异常",
			Type:          AlertRuleTypeLogKeyword,
			AlertSeverity: AlertSeverityCritical,
			Enabled:       true,
			SandboxID:     sandboxID,
			LogKeyword:    "OutOfMemoryError",
			ForDuration:   0,
			Labels:        backendLabels,
		},

		{
			ID:            fmt.Sprintf("default-%s-be-npe", sandboxID),
			Name:          "NPE 异常",
			Type:          AlertRuleTypeLogKeyword,
			AlertSeverity: AlertSeverityWarning,
			Enabled:       true,
			SandboxID:     sandboxID,
			LogKeyword:    "NullPointerException",
			ForDuration:   0,
			Labels:        backendLabels,
		},
	}
}
