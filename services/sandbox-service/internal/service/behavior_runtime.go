// behavior_runtime.go 行为模拟运行时（行为模拟第一批）。
//
// BehaviorRuntime 负责行为模拟工作负载在单个沙箱内的启停与状态观测，
// 不保留跨进程状态：重启后靠 GetBehaviorWorkload 对实际状态自愈。
//
// 本地降级：K8sClient 未连真实集群（fake clientset）时，Start 不部署
// 工作负载，只把阶段标记为 degraded 并返回降级原因；行为信号在降级
// 环境绝不产生，避免页面出现假监控。
package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
)

// 行为模拟运行阶段。
const (
	BehaviorPhaseStopped  = "stopped"  // 未部署/已停止
	BehaviorPhaseStarting = "starting" // 工作负载已提交，等待就绪
	BehaviorPhaseRunning  = "running"  // 至少一个副本就绪
	BehaviorPhaseDegraded = "degraded" // 本地 fake 降级，未真正运行
	BehaviorPhaseFailed   = "failed"   // 已部署但全部副本不可用
)

// BehaviorStartConfig 启动行为模拟的参数。
type BehaviorStartConfig struct {
	CommandURL   string
	RunnerSecret string
	// SessionID 实训会话ID（必填）。
	SessionID int64
	// EntryURL 被测项目前端入口地址；为空时允许提交但记录警告。
	EntryURL string
	// SignalEndpoint 行为信号汇聚端点；第一批可留空。
	SignalEndpoint string
	// Image 行为工作负载镜像；为空使用默认镜像。
	Image string
	// Flow 行为流运行配置（真机必填，runner 的 flow.json 来源）。
	// fake 降级时忽略（不创建任何资源）。
	Flow *BehaviorFlowConfig
}

// BehaviorStatus 行为模拟运行状态。
type BehaviorStatus struct {
	// SessionID 实训会话ID。
	SessionID int64 `json:"session_id"`
	// Phase 运行阶段，见 BehaviorPhase* 常量。
	Phase string `json:"phase"`
	// ReadyReplicas 就绪副本数。
	ReadyReplicas int32 `json:"ready_replicas"`
	// Replicas 期望副本数。
	Replicas int32 `json:"replicas"`
	// EntryURL 被测前端入口地址。
	EntryURL string `json:"entry_url"`
	// Reason 降级/失败原因；正常运行为空。
	Reason string `json:"reason,omitempty"`
	// StartedAt 本次启动时间（RFC3339）；未启动为空。
	StartedAt string `json:"started_at,omitempty"`
}

// BehaviorRuntime 行为模拟运行时。
type BehaviorRuntime struct {
	kc *K8sClient

	mu        sync.Mutex
	startedAt map[int64]time.Time
}

// NewBehaviorRuntime 构造行为模拟运行时。
func NewBehaviorRuntime(kc *K8sClient) *BehaviorRuntime {
	return &BehaviorRuntime{
		kc:        kc,
		startedAt: map[int64]time.Time{},
	}
}

// Start 启动指定沙箱的行为模拟（幂等：已在运行直接返回当前状态）。
// 本地 fake 降级时不触达集群，阶段标记为 degraded。
func (r *BehaviorRuntime) Start(ctx context.Context, cfg BehaviorStartConfig) (*BehaviorStatus, error) {
	if cfg.SessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}

	r.mu.Lock()
	if _, ok := r.startedAt[cfg.SessionID]; !ok {
		r.startedAt[cfg.SessionID] = time.Now()
	}
	startedAt := r.startedAt[cfg.SessionID]
	r.mu.Unlock()

	if !r.kc.RealCluster() {
		log.Printf("[behavior] 会话 %d 本地 fake 降级：不部署行为模拟工作负载，行为信号不产生", cfg.SessionID)
		return &BehaviorStatus{
			SessionID: cfg.SessionID,
			Phase:     BehaviorPhaseDegraded,
			EntryURL:  cfg.EntryURL,
			Reason:    "当前为本地 fake 降级环境，行为模拟需要 Kubernetes 集群",
			StartedAt: startedAt.Format(time.RFC3339),
		}, nil
	}

	if cfg.Flow == nil {
		return nil, fmt.Errorf("缺少行为流配置，无法启动行为模拟")
	}
	if cfg.EntryURL == "" {
		cfg.EntryURL = cfg.Flow.EntryURL
	}
	if cfg.EntryURL == "" {
		return nil, fmt.Errorf("缺少前端入口地址，无法启动行为模拟")
	}

	// 先写 runner 脚本与 flow 两个 ConfigMap，再提交 Deployment：
	// Pod 启动后会等待 runner.js，顺序不能颠倒。
	if err := r.kc.ApplyBehaviorConfigMaps(ctx, cfg.SessionID, cfg.Flow); err != nil {
		return nil, fmt.Errorf("写入行为模拟配置失败: %w", err)
	}

	if _, err := r.kc.ApplyBehaviorWorkload(ctx, BehaviorWorkloadSpec{
		SessionID:      cfg.SessionID,
		Image:          cfg.Image,
		EntryURL:       cfg.EntryURL,
		SignalEndpoint: cfg.SignalEndpoint,
		ReplicaCount:   1,
		CommandURL:     cfg.CommandURL,
		RunnerSecret:   cfg.RunnerSecret,
	}); err != nil {
		return nil, fmt.Errorf("启动行为模拟失败: %w", err)
	}

	return r.Status(ctx, cfg.SessionID)
}

// Stop 停止指定沙箱的行为模拟（幂等）。
// namespace 随沙箱释放会整体删除，这里提供显式停止用于沙箱释放前的兜底。
func (r *BehaviorRuntime) Stop(ctx context.Context, sessionID int64) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID非法")
	}
	r.mu.Lock()
	delete(r.startedAt, sessionID)
	r.mu.Unlock()

	if err := r.kc.DeleteBehaviorWorkload(ctx, sessionID); err != nil {
		return fmt.Errorf("停止行为模拟失败: %w", err)
	}
	// ConfigMap 会随 namespace 整体删除回收，这里显式删除作为提前释放的兜底；
	// fake 降级环境从未创建过，Delete 自身对不存在对象幂等。
	if err := r.kc.DeleteBehaviorConfigMaps(ctx, sessionID); err != nil {
		return fmt.Errorf("清理行为模拟配置失败: %w", err)
	}
	return nil
}

// Status 查询指定沙箱的行为模拟状态。
// 工作负载不存在时返回 stopped；本地降级且曾 Start 过返回 degraded。
func (r *BehaviorRuntime) Status(ctx context.Context, sessionID int64) (*BehaviorStatus, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}

	r.mu.Lock()
	startedAt, started := r.startedAt[sessionID]
	r.mu.Unlock()

	dep, err := r.kc.GetBehaviorWorkload(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if dep == nil {
		phase := BehaviorPhaseStopped
		reason := ""
		// 服务重启后内存标记丢失：fake 环境且无工作负载，直接视为 stopped。
		if !r.kc.RealCluster() && started {
			phase = BehaviorPhaseDegraded
			reason = "当前为本地 fake 降级环境，行为模拟需要 Kubernetes 集群"
		}
		st := &BehaviorStatus{
			SessionID: sessionID,
			Phase:     phase,
			Reason:    reason,
		}
		if started {
			st.StartedAt = startedAt.Format(time.RFC3339)
		}
		return st, nil
	}

	return r.statusFromDeployment(dep, startedAt, started), nil
}

// statusFromDeployment 依据 Deployment 状态推导运行阶段。
func (r *BehaviorRuntime) statusFromDeployment(dep *appsv1.Deployment, startedAt time.Time, started bool) *BehaviorStatus {
	var entryURL string
	for _, env := range dep.Spec.Template.Spec.Containers {
		for _, e := range env.Env {
			if e.Name == "ENTRY_URL" {
				entryURL = e.Value
			}
		}
	}

	st := &BehaviorStatus{
		SessionID:     sessionFromLabels(dep),
		ReadyReplicas: dep.Status.ReadyReplicas,
		Replicas:      dep.Status.Replicas,
		EntryURL:      entryURL,
	}
	if started {
		st.StartedAt = startedAt.Format(time.RFC3339)
	}

	switch {
	case dep.Status.ReadyReplicas > 0:
		st.Phase = BehaviorPhaseRunning
	case dep.Status.Replicas == 0:
		st.Phase = BehaviorPhaseStopped
	default:
		// 期望副本存在但无就绪：提交后短暂窗口为 starting，
		// 超过推进时间仍未就绪交由上层排障（这里统一报 starting，
		// 不做基于墙钟的失败判定，避免与 Deployment 自身状态冲突）。
		st.Phase = BehaviorPhaseStarting
	}
	return st
}

// sessionFromLabels 从工作负载标签取回 sessionID；标签缺失时返回 0。
func sessionFromLabels(dep *appsv1.Deployment) int64 {
	if v, ok := dep.Labels["quenching.io/session"]; ok {
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return 0
}
