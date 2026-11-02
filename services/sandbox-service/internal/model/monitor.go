package model

import "time"

// ResourceMetrics 沙箱资源使用指标（CPU/内存）。
// 从 K8s metrics-server 或 Prometheus 采集。
type ResourceMetrics struct {
	SessionID int64     `json:"session_id"`
	Namespace string    `json:"namespace"`
	Timestamp time.Time `json:"timestamp"`

	// CPU 使用量（毫核，millicores）。
	CPUCoresM float64 `json:"cpu_cores_m"` // e.g. 350 = 0.35 核
	// CPU 使用百分比（相对于沙箱配额）。
	CPUPercent float64 `json:"cpu_percent"` // 0-100

	// 内存使用量（字节）。
	MemoryBytes int64 `json:"memory_bytes"`
	// 内存使用百分比。
	MemoryPercent float64 `json:"memory_percent"` // 0-100

	// Pod 数量。
	PodCount int `json:"pod_count"`
	// Running Pod 数。
	PodRunning int `json:"pod_running"`
	// Failed / Pending Pod 数（异常指标）。
	PodAbnormal int `json:"pod_abnormal"`
}

// AnomalyLevel 异常级别。
type AnomalyLevel string

const (
	AnomalyWarning AnomalyLevel = "warning" // 警告：接近阈值
	AnomalyError   AnomalyLevel = "error"   // 严重：超过阈值，需要干预
)

// AnomalyType 异常类型。
const (
	AnomalyHighCPU     = "high_cpu"     // CPU 过高
	AnomalyHighMemory  = "high_memory"  // 内存过高
	AnomalyLongRunning = "long_running" // 运行时间过长
	AnomalyFailedPod   = "failed_pod"   // Pod 异常
	AnomalyStuckStatus = "stuck_status" // 状态卡住（长时间 creating/deploying）
	AnomalyOOM         = "oom"          // OOM 内存溢出
)

// SandboxAnomaly 沙箱异常记录。
type SandboxAnomaly struct {
	ID          int64        `json:"id"`
	SessionID   int64        `json:"session_id"`
	Namespace   string       `json:"namespace"`
	UserID      int64        `json:"user_id"`
	AnomalyType string       `json:"anomaly_type"`
	Level       AnomalyLevel `json:"level"`
	Message     string       `json:"message"`
	// 异常时的资源快照（JSON 形式存储）。
	Snapshot *ResourceMetrics `json:"snapshot,omitempty"`

	DetectedAt time.Time `json:"detected_at"`
	ResolvedAt time.Time `json:"resolved_at,omitempty"` // 恢复时间（零值=未恢复）
	Resolved   bool      `json:"resolved"`
}

// SandboxQuery 沙箱列表查询参数（管理后台用）。
type SandboxQuery struct {
	// 筛选条件
	UserID    int64  // 按用户筛选
	ProjectID int64  // 按项目筛选
	Status    string // 按状态筛选
	Namespace string // 按命名空间模糊搜索
	IsActive  *bool  // 是否活跃（nil=全部, true=未释放, false=已释放）

	// 分页
	Page     int
	PageSize int
}

// SandboxPage 沙箱分页结果。
type SandboxPage struct {
	Items    []Sandbox `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// SandboxDetail 沙箱详情（含运行时长、资源概览）。
type SandboxDetail struct {
	Sandbox
	// 运行时长（秒）。
	RuntimeSeconds int64 `json:"runtime_seconds"`
	// 剩余存活时间（秒，负数=已过期）。
	RemainingSeconds int64 `json:"remaining_seconds"`
	// 当前资源指标（如果有）。
	Metrics *ResourceMetrics `json:"metrics,omitempty"`
	// 关联项目名。
	ProjectName string `json:"project_name,omitempty"`
	// 关联用户名。
	UserName string `json:"user_name,omitempty"`
}

// AnomalyQuery 异常查询参数。
type AnomalyQuery struct {
	Level       AnomalyLevel // 按级别筛选
	AnomalyType string       // 按类型筛选
	Resolved    *bool        // 是否已解决
	Page        int
	PageSize    int
}

// AnomalyPage 异常分页结果。
type AnomalyPage struct {
	Items    []SandboxAnomaly `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// ComputeRuntime 计算沙箱运行时长（秒）。
func (s *Sandbox) ComputeRuntime(now time.Time) int64 {
	if s.CreatedAt.IsZero() {
		return 0
	}
	end := now
	if !s.CompletedAt.IsZero() {
		end = s.CompletedAt
	}
	dur := end.Sub(s.CreatedAt)
	if dur < 0 {
		return 0
	}
	return int64(dur.Seconds())
}

// ComputeRemaining 计算剩余存活时间（秒），取"距租期到期"与"距绝对上限到期"的较小值。
// 负数表示已过期。
func (s *Sandbox) ComputeRemaining(now time.Time) int64 {
	until := s.activityAnchor().Add(s.lease())
	if !s.CreatedAt.IsZero() {
		if ceiling := s.CreatedAt.Add(MaxSandboxLifetime); ceiling.Before(until) {
			until = ceiling
		}
	}
	return int64(until.Sub(now).Seconds())
}

// IsActiveState 判断状态是否为活跃（非终态）。
func (s *Sandbox) IsActiveState() bool {
	switch s.Status {
	case StatusReleased, StatusFailed:
		return false
	default:
		return true
	}
}
