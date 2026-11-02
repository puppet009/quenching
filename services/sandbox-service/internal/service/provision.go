package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/quenching/go-common/logger"
	"go.uber.org/zap"
)

// ProvisionService 沙箱启动编排服务。
//
// 编排流程：
//  1. 安装 Helm Chart（部署业务系统）
//  2. 等待 Deployment 全部就绪
//  3. 成功 → 返回
//  4. 失败 → 回滚（卸载 Chart），返回错误
//
// 本服务只负责"部署"这一步，K8s namespace/quota/networkpolicy
// 等隔离资源由 Reconciler 负责（在更前面的步骤创建）。
type ProvisionService struct {
	deployer Deployer
	waiter   *ReadyWaiter
}

// NewProvisionService 创建编排服务。
func NewProvisionService(d Deployer, w *ReadyWaiter) *ProvisionService {
	return &ProvisionService{deployer: d, waiter: w}
}

// ProvisionOpts 启动参数。
type ProvisionOpts struct {
	Namespace   string            // 沙箱 namespace
	ReleaseName string            // Helm Release 名
	ChartPath   string            // Chart 路径
	Values      map[string]string // 自定义 values
	Timeout     time.Duration     // 总超时（部署+等待），默认 5min
}

// Provision 执行完整的沙箱启动编排。
// 成功返回 nil，失败返回错误（已自动回滚）。
func (s *ProvisionService) Provision(ctx context.Context, opts ProvisionOpts) error {
	started := time.Now()
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	// 用超时 context 控制整体时间
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 步骤1：安装 Helm Chart
	chart := ChartInfo{
		Name:      opts.ReleaseName,
		Namespace: opts.Namespace,
		ChartPath: opts.ChartPath,
		Values:    opts.Values,
	}

	if err := s.deployer.Install(ctx, chart); err != nil {
		return s.failed(ctx, opts, started, fmt.Errorf("部署 Chart 失败: %w", err))
	}

	// 步骤2：等待就绪
	if s.waiter != nil {
		ready, err := s.waiter.WaitForDeployments(ctx, opts.Namespace)
		if err != nil {
			return s.failed(ctx, opts, started, fmt.Errorf("等待就绪失败 (%d ready): %w", ready, err))
		}
	}

	return nil
}

// Teardown 清理沙箱环境（卸载 Chart）。
// 幂等：已卸载也返回成功。
func (s *ProvisionService) Teardown(ctx context.Context, namespace, release string) error {
	return s.deployer.Uninstall(ctx, namespace, release)
}

// rollback 回滚（卸载 Chart）。
// 用独立 context 避免被外层 ctx cancel 影响（确保回滚一定执行）。
func (s *ProvisionService) rollback(ctx context.Context, namespace, release string) error {
	// 回滚也设超时，避免无限等待
	rbCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	return s.deployer.Uninstall(rbCtx, namespace, release)
}

func (s *ProvisionService) failed(ctx context.Context, opts ProvisionOpts, started time.Time, cause error) error {
	// Preserve startup evidence before Helm rollback removes the failed workloads.
	if s.waiter != nil {
		diagnosticCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		details := s.waiter.StartupDiagnostics(diagnosticCtx, opts.Namespace)
		cancel()
		cause = fmt.Errorf("%w; 启动诊断: %s", cause, details)
	}
	logger.From(ctx).Error("沙箱部署失败，准备回滚", zap.String("namespace", opts.Namespace), zap.String("release", opts.ReleaseName), zap.Duration("elapsed", time.Since(started)), zap.Error(cause))
	if err := s.rollback(context.WithoutCancel(ctx), opts.Namespace, opts.ReleaseName); err != nil {
		logger.From(ctx).Error("沙箱部署回滚失败", zap.String("namespace", opts.Namespace), zap.String("release", opts.ReleaseName), zap.Error(err))
		cause = errors.Join(cause, fmt.Errorf("回滚失败: %w", err))
	}
	return cause
}
