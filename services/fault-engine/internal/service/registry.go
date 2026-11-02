// registry.go：SupervisorRegistry 管理多个进行中实训的 FaultSupervisor。
//
// 一个训练会话对应一个 supervisor；会话开始时 Start，结束/判定完成时自动退出，
// 也可由外部 StopSession 强制停止。集中管理防止 goroutine 泄漏和重复编排。
package service

import (
	"context"
	"fmt"
	"sync"
)

// Runner 抽象 supervisor 的运行（便于测试用同步 fake 替换真实 goroutine 循环）。
type Runner interface {
	Run(ctx context.Context)
	Stop()
	Done() <-chan struct{}
	Phase() Phase
}

// SupervisorRegistry 会话级 supervisor 注册表。
type SupervisorRegistry struct {
	mu      sync.Mutex
	runners map[int64]Runner
}

func NewSupervisorRegistry() *SupervisorRegistry {
	return &SupervisorRegistry{runners: make(map[int64]Runner)}
}

// Start 为某个会话启动 supervisor。重复启动同一会话返回错误（防止双重注入）。
// supervisor 在独立 goroutine 中运行；其进入终态后由调用方按需清理（见 Cleanup）。
func (r *SupervisorRegistry) Start(parentCtx context.Context, sessionID int64, runner Runner) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runners[sessionID]; exists {
		return fmt.Errorf("会话 %d 的故障编排已在运行", sessionID)
	}
	r.runners[sessionID] = runner

	go func() {
		runner.Run(parentCtx)
	}()
	return nil
}

// StopSession 外部强制停止某会话（实训提前结束/超时），会触发其 Abort 撤销故障。
func (r *SupervisorRegistry) StopSession(sessionID int64) bool {
	r.mu.Lock()
	runner, ok := r.runners[sessionID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	runner.Stop()
	return true
}

// Cleanup 移除已结束（Done）的会话，释放注册表项。建议定时调用。
func (r *SupervisorRegistry) Cleanup() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var removed []int64
	for id, runner := range r.runners {
		select {
		case <-runner.Done():
			delete(r.runners, id)
			removed = append(removed, id)
		default:
		}
	}
	return removed
}

// Has 某会话是否仍在编排。
func (r *SupervisorRegistry) Has(sessionID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.runners[sessionID]
	return ok
}

// ActiveCount 当前活跃编排数（监控用）。
func (r *SupervisorRegistry) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runners)
}

// StopAll 停止全部（服务优雅退出时调用，统一撤销所有进行中的故障）。
func (r *SupervisorRegistry) StopAll() {
	r.mu.Lock()
	runners := make([]Runner, 0, len(r.runners))
	for _, rn := range r.runners {
		runners = append(runners, rn)
	}
	r.mu.Unlock()
	for _, rn := range runners {
		rn.Stop()
	}
}

// StopSessionAndWait confirms the legacy supervisor has stopped mutating experiments.
func (r *SupervisorRegistry) StopSessionAndWait(ctx context.Context, id int64) (Phase, bool, error) {
	r.mu.Lock()
	runner, ok := r.runners[id]
	r.mu.Unlock()
	if !ok {
		return "", false, nil
	}
	runner.Stop()
	select {
	case <-ctx.Done():
		return "", true, ctx.Err()
	case <-runner.Done():
		return runner.Phase(), true, nil
	}
}
