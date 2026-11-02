// 踩坑后的次生故障（后果传导，T3-12）。
//
// 边界：本文件只做"配置 -> 次生故障注入规格"的纯逻辑解析与强度归一，
// 不碰数据库、不调故障引擎。触发时机（agree/fell 首次判定）在编排层，
// 真正注入/撤销走 service.ConsequenceExecutor 端口（云上对 fault-engine 运行时 API）。
//
// 语义（任务卡 T3-12 / 《技术架构文档》§11.6、§5.1）：
//   - 用户采纳错误建议（agree）且 worsen_fault=true 时，联动注入一次次生故障；
//   - 典型映射：错误命令 -> 额外负载（cpu 满载），错误配置 -> 错误率上升
//     （network_delay/network_loss 导致请求超时/失败）；
//   - 后果必须可观察：注入经由 ChaosBlade 制造真实负载/丢包，监控面指标自然恶化；
//   - 后果必须可恢复：平台掌控时长（duration_sec=0 不自动恢复），会话结束时统一撤销。
//
// 故障类型/强度字段与 fault-engine internal/model.InjectSpec 保持同名同义，
// 但 training 侧不直接依赖 fault-engine（其代码均在 internal/ 下，跨模块不可导入），
// 两侧通过 HTTP 契约（JSON 字段名）解耦。
package model

import "fmt"

// 次生故障类型（与 fault-engine model.FaultKind 字符串一致，跨服务按字符串约定）。
// 只开放"制造恶化、可平滑撤销"的负载/网络类；process kill 属破坏而非恶化，不在此列。
const (
	ConsequenceCPU      = "cpu"           // CPU 满载（错误命令致额外负载）
	ConsequenceMemory   = "memory"        // 内存占用
	ConsequenceDisk     = "disk"          // 磁盘填充
	ConsequenceNetDelay = "network_delay" // 网络延迟（错误配置致超时/错误率上升）
	ConsequenceNetLoss  = "network_loss"  // 网络丢包（错误配置致错误率上升）
)

var supportedConsequenceKind = map[string]bool{
	ConsequenceCPU:      true,
	ConsequenceMemory:   true,
	ConsequenceDisk:     true,
	ConsequenceNetDelay: true,
	ConsequenceNetLoss:  true,
}

// severity 缺省强度（配置只给类型不给强度时按危险程度归一）。
var (
	severityCPU  = map[string]int{SeverityLow: 60, SeverityMedium: 80, SeverityHigh: 100}
	severityMem  = map[string]int{SeverityLow: 60, SeverityMedium: 75, SeverityHigh: 90}
	severityDisk = map[string]int{SeverityLow: 50, SeverityMedium: 70, SeverityHigh: 90}
	severityMS   = map[string]int{SeverityLow: 200, SeverityMedium: 500, SeverityHigh: 1000}
	severityLoss = map[string]int{SeverityLow: 10, SeverityMedium: 30, SeverityHigh: 50}
)

// ConsequenceFault 一次次生故障的注入规格（pitfall_rules.consequence JSONB）。
// 只描述"制造什么故障、多强"；注入目标（namespace/pod）由云上执行通道按
// 会话沙箱定位，持续时间 0 表示平台掌控、会话结束时撤销。
type ConsequenceFault struct {
	// Kind 故障类型，取值见 Consequence* 常量。
	Kind string `json:"kind"`
	// 强度参数（各类型按需取，0 表示按 severity 取缺省强度）。
	CPUPercent  int `json:"cpu_percent,omitempty"`
	MemPercent  int `json:"mem_percent,omitempty"`
	DiskPercent int `json:"disk_percent,omitempty"`
	DelayMillis int `json:"delay_millis,omitempty"`
	LossPercent int `json:"loss_percent,omitempty"`
	// DurationSec 自动恢复秒数；0=不自动恢复，由平台在会话结束时撤销（默认，推荐）。
	DurationSec int `json:"duration_sec,omitempty"`
}

// Validate 校验次生故障规格合法性（加载期 fail-fast，避免训练中途才暴露配置问题）。
func (q *ConsequenceFault) Validate(configID int64) error {
	if q == nil {
		return nil
	}
	if !supportedConsequenceKind[q.Kind] {
		return fmt.Errorf("坑点配置 %d 次生故障类型非法: %q", configID, q.Kind)
	}
	if q.CPUPercent < 0 || q.CPUPercent > 100 ||
		q.MemPercent < 0 || q.MemPercent > 100 ||
		q.DiskPercent < 0 || q.DiskPercent > 100 ||
		q.LossPercent < 0 || q.LossPercent > 100 {
		return fmt.Errorf("坑点配置 %d 次生故障强度百分比必须在 [0,100]", configID)
	}
	if q.DelayMillis < 0 || q.DurationSec < 0 {
		return fmt.Errorf("坑点配置 %d 次生故障时长参数不能为负", configID)
	}
	return nil
}

// ResolveConsequence 解析踩坑后应联动注入的次生故障规格。
//
// 返回 nil 表示该坑点不做后果传导（worsen_fault=false）。
// worsen_fault=true 但未显式配置 consequence 时，默认按"错误命令致额外负载"
// 注入 CPU 满载；显式给了类型但强度留 0 时，按 cfg.Severity 取缺省强度。
// 返回值总是已填齐该类型所需强度的规格，可直接交给执行通道。
func (f FallRules) ResolveConsequence(severity string) *ConsequenceFault {
	if !f.WorsenFault {
		return nil
	}
	q := f.Consequence
	if q == nil {
		q = &ConsequenceFault{Kind: ConsequenceCPU}
	}
	q.fillBySeverity(normalizeSeverity(severity))
	return q
}

// fillBySeverity 按类型补齐缺省强度；显式配置的非零强度不被覆盖。
func (q *ConsequenceFault) fillBySeverity(sev string) {
	switch q.Kind {
	case ConsequenceCPU:
		q.CPUPercent = defaultNonZero(q.CPUPercent, severityCPU[sev])
	case ConsequenceMemory:
		q.MemPercent = defaultNonZero(q.MemPercent, severityMem[sev])
	case ConsequenceDisk:
		q.DiskPercent = defaultNonZero(q.DiskPercent, severityDisk[sev])
	case ConsequenceNetDelay:
		q.DelayMillis = defaultNonZero(q.DelayMillis, severityMS[sev])
	case ConsequenceNetLoss:
		q.LossPercent = defaultNonZero(q.LossPercent, severityLoss[sev])
	}
}

func normalizeSeverity(sev string) string {
	if sev == SeverityLow || sev == SeverityHigh {
		return sev
	}
	return SeverityMedium
}

func defaultNonZero(v, d int) int {
	if v != 0 {
		return v
	}
	return d
}
