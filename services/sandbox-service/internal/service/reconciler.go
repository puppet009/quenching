// Package service 沙箱业务编排。
// 调谐器(reconciler)是沙箱防泄漏的核心：不依赖内存状态，每次都拿"期望状态(平台DB)"
// 对齐"实际状态(K8s)"，服务重启后靠重新调谐自愈。所有操作幂等。
package service

import (
	"context"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
)

// Store 平台侧沙箱记录的存储接口（生产用 PG，单测用 fake）。
type Store interface {
	// ListActive 返回 DB 中所有未终态的沙箱（期望存在于集群，供调谐补建）。
	ListActive(ctx context.Context) ([]model.Sandbox, error)
	// ListAllForReap 返回所有未释放的沙箱（供 TTL 回收扫描，含已结束/超时的）。
	ListAllForReap(ctx context.Context) ([]model.Sandbox, error)
	// MarkStatus 更新沙箱状态。
	MarkStatus(ctx context.Context, sessionID int64, status string) error
	// CountActiveByUser 统计指定用户的活跃沙箱数（未释放且未失败的）。
	// 用于并发配额校验。
	CountActiveByUser(ctx context.Context, userID int64) (int, error)
	// Create 新建沙箱记录。
	Create(ctx context.Context, sb *model.Sandbox) error
}

// Reconciler 沙箱调谐器。
type Reconciler struct {
	kc          *K8sClient
	db          Store
	behavior    *BehaviorRuntime     // 可选：释放沙箱前显式停止行为模拟
	behavSignal *BehaviorSignalStore // 可选：释放时清空该会话信号缓冲
}

func NewReconciler(kc *K8sClient, db Store) *Reconciler {
	return &Reconciler{kc: kc, db: db}
}

// SetBehaviorRuntime 注入行为模拟运行时（释放前兜底停止用）。
func (r *Reconciler) SetBehaviorRuntime(rt *BehaviorRuntime) {
	r.behavior = rt
}

// SetBehaviorSignalStore 注入行为信号汇聚存储（释放沙箱时清缓冲用）。
func (r *Reconciler) SetBehaviorSignalStore(store *BehaviorSignalStore) {
	r.behavSignal = store
}

// ReconcileOnce 执行一轮调谐：
//  1. 期望存在的沙箱（DB active）→ 确保其 namespace 等隔离资源存在（幂等创建）
//  2. 集群中孤儿 namespace（DB 已无记录或已终态）→ 删除，回收泄漏资源
//
// 该方法可被定时器周期性调用，也可在服务启动时调用（自愈重启前残留）。
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	active, err := r.db.ListActive(ctx)
	if err != nil {
		return err
	}
	want := make(map[string]bool, len(active))
	for _, sb := range active {
		want[sb.Namespace] = true
	}

	// 1) 确保期望沙箱的隔离资源存在（已存在则幂等跳过）。
	for _, sb := range active {
		if !sb.CompletedAt.IsZero() || sb.Status == model.StatusReleasing {
			if err := r.kc.CloseSandboxAccess(ctx, sb.Namespace); err != nil {
				return err
			}
			continue
		}
		if sb.Status == model.StatusQueued {
			continue
		}
		if _, err := r.EnsureSandbox(ctx, &sb); err != nil {
			return err
		}
	}

	// 2) 回收孤儿：集群里带沙箱标签、但 DB 不再期望的 namespace。
	orphans, err := r.kc.ListSandboxNamespaces(ctx)
	if err != nil {
		return err
	}
	for _, ns := range orphans {
		if !want[ns] {
			if err := r.kc.DeleteSandbox(ctx, ns); err != nil {
				return err
			}
		}
	}
	return nil
}

// EnsureSandbox 创建/确保单个沙箱的隔离资源（对应 T1-4 创建流程的第一步）。
func (r *Reconciler) EnsureSandbox(ctx context.Context, sb *model.Sandbox) (string, error) {
	var namespace string
	ensure := func(ctx context.Context) (err error) {
		if sb.ResourcePlan.Requests.CPUMilli > 0 {
			namespace, err = r.kc.EnsureSandboxResourcesWithPlan(ctx, itoa(sb.SessionID), itoa(sb.UserID), sb.ResourcePlan)
		} else {
			namespace, err = r.kc.EnsureSandboxResources(ctx, itoa(sb.SessionID), itoa(sb.UserID))
		}
		return err
	}
	// Production PGStore holds the sandbox row lock only around resource/access
	// creation, so MarkCompleted cannot acknowledge before an in-flight grant.
	if gate, ok := r.db.(interface {
		WithWorkspaceAccess(context.Context, int64, func(context.Context) error) error
	}); ok {
		err := gate.WithWorkspaceAccess(ctx, sb.SessionID, ensure)
		return namespace, err
	}
	err := ensure(ctx)
	return namespace, err
}

// K8sClient 暴露底层 K8s 客户端，供需要直接操作 K8s 的业务服务使用。
func (r *Reconciler) K8sClient() *K8sClient {
	return r.kc
}

// ReleaseSandbox 释放单个沙箱（删 namespace）。
func (r *Reconciler) ReleaseSandbox(ctx context.Context, sb *model.Sandbox) error {
	r.stopBehavior(ctx, sb.SessionID)
	if err := r.kc.DeleteSandbox(ctx, sb.Namespace); err != nil {
		return err
	}
	if r.behavSignal != nil {
		r.behavSignal.Clear(sb.SessionID)
	}
	return r.db.MarkStatus(ctx, sb.SessionID, model.StatusReleased)
}

// stopBehavior 显式停止行为模拟；失败只记录不阻塞释放
// （namespace 删除会连同 Pod 一起清理，这只是兜底）。
func (r *Reconciler) stopBehavior(ctx context.Context, sessionID int64) {
	if r.behavior == nil {
		return
	}
	if err := r.behavior.Stop(ctx, sessionID); err != nil {
		// 不返回错误：namespace 删除是最终清理手段。
		_ = err
	}
}

// ReapExpired 扫描并回收所有已过期沙箱（TTL 定时任务调用）。
// 判定纯逻辑在 model.Sandbox.IsExpired，这里只负责执行删除+回写状态。
// 返回被回收的沙箱 sessionID 列表，便于上层打日志/发指标。
// now 由调用方传入（定时器传 time.Now()，单测传固定时间），保证可测试。
func (r *Reconciler) ReapExpired(ctx context.Context, now time.Time) ([]int64, error) {
	all, err := r.db.ListAllForReap(ctx)
	if err != nil {
		return nil, err
	}
	var reaped []int64
	for i := range all {
		sb := all[i]
		if !sb.IsExpired(now) {
			continue
		}
		r.stopBehavior(ctx, sb.SessionID)
		if err := r.kc.DeleteSandbox(ctx, sb.Namespace); err != nil {
			return reaped, err
		}
		if r.behavSignal != nil {
			r.behavSignal.Clear(sb.SessionID)
		}
		if err := r.db.MarkStatus(ctx, sb.SessionID, model.StatusReleased); err != nil {
			return reaped, err
		}
		reaped = append(reaped, sb.SessionID)
	}
	return reaped, nil
}

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
