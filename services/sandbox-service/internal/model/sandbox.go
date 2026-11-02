// Package model 沙箱领域模型。
// 一次实训 = 一个独立 Kubernetes Namespace（sandbox-<sessionId>）。
// 本包只定义模型与状态机，不依赖 client-go，便于单测。
package model

import "time"

// 沙箱状态。
const (
	StatusQueued    = "queued"
	StatusCreating  = "creating"  // 正在建 namespace / 资源
	StatusDeploying = "deploying" // 正在部署模板项目
	StatusReady     = "ready"     // 环境就绪，等待进入实训
	StatusRunning   = "running"   // 实训进行中
	StatusReleasing = "releasing" // 正在释放
	StatusReleased  = "released"  // 已释放（namespace 已删）
	StatusFailed    = "failed"    // 部署/运行失败
)

// 沙箱单实例资源上限（对应需求：CPU<2核、内存<4G）。
const (
	MaxSandboxCPUCores = 2
	MaxSandboxMemoryGi = 4
	// 单用户并发沙箱数。
	MaxSandboxPerUser = 1
)

// 回收时间口径。
const (
	// ActivityLease 无活动租期：距最后一次实训活动超过该时长即判弃班并回收。
	// 大于 training-service 的 20 分钟弃班阈值，作为 training 不可用时的兜底。
	ActivityLease = 30 * time.Minute
	// MaxSandboxLifetime 从创建起算的绝对上限：班次不硬超时，学员可以一直做到完成，
	// 该上限只在活跃度信号异常时阻止资源永驻（单机沙箱槽位是稀缺资源）。
	MaxSandboxLifetime = 4 * time.Hour
	// ReleaseGraceAfterComplete 实训结束后宽限期：给复盘生成/报告查看留时间，过后释放。
	ReleaseGraceAfterComplete = 5 * time.Minute
	// ReapScanInterval 回收扫描周期。
	ReapScanInterval = 1 * time.Minute
)

// Sandbox 沙箱实例。
type Sandbox struct {
	TemplateSlug      string       `json:"template_slug,omitempty"`
	TemplateDigest    string       `json:"template_digest,omitempty"`
	ResourcePlan      ResourcePlan `json:"resource_plan"`
	ResourcesAdmitted bool         `json:"resources_admitted"`
	AllocationReason  string       `json:"allocation_reason,omitempty"`
	ID                int64        `json:"id"`
	SessionID         int64        `json:"session_id"` // 关联训练会话
	UserID            int64        `json:"user_id"`
	ProjectID         int64        `json:"project_id"`
	// Namespace 该沙箱在 K8s 中的命名空间名，也是与外部系统对接的关键标识。
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	// 平台与沙箱内服务通信的访问入口（Ingress host / NodePort），部署完成后回填。
	AccessURL string `json:"access_url,omitempty"`
	// CreatedAt 创建时间（绝对上限起算点）。
	CreatedAt time.Time `json:"created_at"`
	// CompletedAt 实训结束时间（判定解决/用户主动结束/失败/弃班）；零值表示未结束。
	CompletedAt time.Time `json:"completed_at,omitempty"`
	// LastActivityAt 最后一次实训活动（终端输入或观察类操作）时间；零值回退创建时间。
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	// Lease 该沙箱的无活动租期；零值用 ActivityLease。
	Lease time.Duration `json:"-"`
}

// lease 取有效无活动租期。
func (s *Sandbox) lease() time.Duration {
	if s.Lease > 0 {
		return s.Lease
	}
	return ActivityLease
}

// activityAnchor 活动锚点：取创建时间与最后活动时间中的较晚者。
// 无活动记录时等同创建时间。
func (s *Sandbox) activityAnchor() time.Time {
	if s.LastActivityAt.After(s.CreatedAt) {
		return s.LastActivityAt
	}
	return s.CreatedAt
}

// IsExpired 判断在 now 时刻沙箱是否应被回收。三种情况（满足任一即回收）：
//  1. 实训已结束（CompletedAt 非零）且超过结束宽限期；
//  2. 距最后一次活动超过租期（活跃即续租；覆盖弃班、客户端崩溃、训练服务未回写终态）；
//  3. 从创建起超过绝对上限（活跃度信号失效时的兜底，防资源永驻）。
func (s *Sandbox) IsExpired(now time.Time) bool {
	if s.Status == StatusReleased {
		return false // 已释放不再处理
	}
	if !s.CompletedAt.IsZero() && now.Sub(s.CompletedAt) >= ReleaseGraceAfterComplete {
		return true
	}
	anchor := s.activityAnchor()
	if !anchor.IsZero() && now.Sub(anchor) >= s.lease() {
		return true
	}
	if !s.CreatedAt.IsZero() && now.Sub(s.CreatedAt) >= MaxSandboxLifetime {
		return true
	}
	return false
}

// NamespaceName 规则：sandbox-<sessionId>。
// 用 sessionId 而非随机串，保证同一实训的沙箱名可重算（调谐幂等的关键）。
func NamespaceName(sessionID int64) string {
	return "sandbox-" + itoa(sessionID)
}

// 简易 int64->string，避免在 model 包引入 strconv 的额外噪音。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
