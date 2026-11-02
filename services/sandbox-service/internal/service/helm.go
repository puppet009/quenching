// helm.go 沙箱环境部署。
//
// MVP 阶段封装 helm CLI 完成 Chart 安装/卸载/升级。
// 选择 CLI 而非 Helm SDK 的原因：
//   - 依赖更轻（不需要拉整个 helm 源码树）
//   - 与本地开发体验一致（helm install 等价）
//   - 后续要换 SDK 只需实现同一接口

package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ChartInfo 描述一个待部署的 Helm Chart。
type ChartInfo struct {
	Name      string            // Release 名称
	Namespace string            // 目标命名空间
	ChartPath string            // Chart 本地路径（或仓库名/chart名）
	Values    map[string]string // 自定义 values（扁平 key=value，简单场景够用）
	Version   string            // Chart 版本（可选）
}

// Deployer Helm 部署器接口。
// 抽象为接口便于单测 mock，以及未来替换为 Helm SDK。
type Deployer interface {
	// Install 安装一个 Helm Release。已存在则返回错误（不自动升级）。
	Install(ctx context.Context, chart ChartInfo) error
	// Uninstall 卸载一个 Helm Release。不存在返回 nil（幂等）。
	Uninstall(ctx context.Context, namespace, release string) error
	// Exists 检查 Release 是否存在。
	Exists(ctx context.Context, namespace, release string) (bool, error)
}

// HelmCLI 通过 helm CLI 操作的部署器。
type HelmCLI struct {
	BinPath string // helm 二进制路径，默认 "helm"
}

// NewHelmCLI 创建默认 helm CLI 部署器。
func NewHelmCLI() *HelmCLI {
	return &HelmCLI{BinPath: "helm"}
}

// Install 执行 helm install。
func (h *HelmCLI) Install(ctx context.Context, chart ChartInfo) error {
	started := time.Now()
	args := []string{
		"install", chart.Name, chart.ChartPath,
		"--namespace", chart.Namespace,
		"--create-namespace",
		"--wait", // 等待就绪（MVP 阶段先靠 helm --wait 兜底）
	}

	if chart.Version != "" {
		args = append(args, "--version", chart.Version)
	}

	for k, v := range chart.Values {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, h.BinPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		kind := "失败"
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
			kind = "已取消"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				kind = "超时"
			}
		}
		return fmt.Errorf("helm install %s (namespace=%s release=%s elapsed=%s): %w, 输出: %s", kind, chart.Namespace, chart.Name, time.Since(started).Round(time.Millisecond), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Uninstall 执行 helm uninstall。
func (h *HelmCLI) Uninstall(ctx context.Context, namespace, release string) error {
	args := []string{
		"uninstall", release,
		"--namespace", namespace,
	}

	cmd := exec.CommandContext(ctx, h.BinPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 不存在则视为成功（幂等）
		if strings.Contains(string(output), "not found") {
			return nil
		}
		return fmt.Errorf("helm uninstall 失败: %w, 输出: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Exists 检查 Release 是否存在（helm status 能查到就算存在）。
func (h *HelmCLI) Exists(ctx context.Context, namespace, release string) (bool, error) {
	args := []string{
		"status", release,
		"--namespace", namespace,
	}

	cmd := exec.CommandContext(ctx, h.BinPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Kubernetes/network/RBAC errors are uncertainty, never evidence of absence.
		if strings.TrimSpace(string(output)) == "Error: release: not found" {
			return false, nil
		}
		return false, fmt.Errorf("helm status 失败: %w", err)
	}
	return true, nil
}
