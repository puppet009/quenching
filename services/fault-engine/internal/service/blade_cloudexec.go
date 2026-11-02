// blade_cloudexec.go 生产环境 blade 执行通道：
// kubectl exec 进 ChaosBlade daemonset agent Pod 执行 blade CLI。
//
// 链路（T1-26/27）：
//
//  1. 按 spec.TargetNamespace/TargetPod 解析目标 Pod 所在 Node；
//  2. 在 AgentNamespace 下按 AgentPodSelector 选出运行在该 Node 的 agent Pod
//     （ChaosBlade 以 DaemonSet 部署，每节点一个）；解析不到目标 Pod 时退化为任意一个 agent；
//  3. kubectl exec <agentPod> -n <ns> -c <container> -- <bladePath> <args...>；
//  4. stdout 原样放入 BladeExecResult.Raw，blade uid 的解析交给上层 Injector（ParseBladeUID）。
//
// 本文件不拼装任何业务参数：create/destroy/status 的 args 全部来自 blade_command.go 的纯函数，
// 保持"命令构造"与"命令执行"分离，无集群机器上仍可单测命令构造与参数拼接。

package service

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/quenching/fault-engine/internal/model"
)

// BladeCloudExecConfig kubectl exec 通道配置（configs/config.yaml 的 chaos 段）。
type BladeCloudExecConfig struct {
	Enabled      bool
	OperatorName string
	// KubectlPath kubectl 可执行文件路径，默认 "kubectl"（走 PATH）。
	KubectlPath string
	// Kubeconfig 显式 kubeconfig 路径；为空使用服务账号/POD 内默认凭证。
	Kubeconfig string
	// AgentNamespace agent DaemonSet 所在命名空间，默认 "chaos"。
	AgentNamespace string
	// AgentPodSelector agent Pod 标签选择器，默认 "app=chaosblade-agent"。
	AgentPodSelector string
	// Container agent Pod 内 blade 所在容器名，默认 "chaosblade"。
	Container string
	// BladePath agent 容器内 blade 可执行路径，默认 "/opt/chaosblade/blade"。
	BladePath string
}

// BladeCloudExec 生产执行通道。
type BladeCloudExec struct {
	cfg BladeCloudExecConfig
}

// 编译期断言：BladeCloudExec 满足 BladeExecutor。
var _ BladeExecutor = (*BladeCloudExec)(nil)

// NewBladeCloudExec 创建执行通道并填充默认值。
func NewBladeCloudExec(cfg BladeCloudExecConfig) *BladeCloudExec {
	if cfg.OperatorName == "" {
		cfg.OperatorName = "chaosblade-operator"
	}
	if cfg.KubectlPath == "" {
		cfg.KubectlPath = "kubectl"
	}
	if cfg.AgentNamespace == "" {
		cfg.AgentNamespace = "chaos"
	}
	if cfg.AgentPodSelector == "" {
		cfg.AgentPodSelector = "app=chaosblade-agent"
	}
	if cfg.Container == "" {
		cfg.Container = "chaosblade"
	}
	if cfg.BladePath == "" {
		cfg.BladePath = "/opt/chaosblade/blade"
	}
	return &BladeCloudExec{cfg: cfg}
}

// Run 在目标 Pod 同节点的 agent 上执行 blade 命令。
func (c *BladeCloudExec) Run(ctx context.Context, spec model.InjectSpec, args []string) (*BladeExecResult, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("blade 执行参数为空")
	}

	agentPod, err := c.resolveAgentPod(ctx, spec)
	if err != nil {
		return nil, err
	}

	// exec agentPod -n <ns> -c <container> -- <bladePath> <args...>
	execArgs := []string{"exec", agentPod, "-n", c.cfg.AgentNamespace}
	if c.cfg.Container != "" {
		execArgs = append(execArgs, "-c", c.cfg.Container)
	}
	execArgs = append(execArgs, "--", c.cfg.BladePath)
	execArgs = append(execArgs, args...)

	raw, err := c.runKubectl(ctx, execArgs)
	if err != nil {
		return nil, err
	}
	return &BladeExecResult{Raw: raw}, nil
}

// resolveAgentPod 选出承载本次注入的 agent Pod：目标 Pod 所在节点的 agent 优先。
func (c *BladeCloudExec) resolveAgentPod(ctx context.Context, spec model.InjectSpec) (string, error) {
	targetNode := ""
	if spec.TargetNamespace != "" && spec.TargetPod != "" {
		out, err := c.runKubectl(ctx, []string{
			"get", "pod", spec.TargetPod,
			"-n", spec.TargetNamespace,
			"-o", "jsonpath={.spec.nodeName}",
		})
		if err == nil {
			targetNode = strings.TrimSpace(out)
		}
		// 目标 Pod 取不到（临时直注可能只给命名空间）不致命：退化为任意 agent。
	}

	pods, err := c.listAgentPods(ctx)
	if err != nil {
		return "", err
	}
	if len(pods) == 0 {
		return "", fmt.Errorf("命名空间 %s 下选择器 %s 未找到 ChaosBlade agent Pod",
			c.cfg.AgentNamespace, c.cfg.AgentPodSelector)
	}
	if targetNode != "" {
		nodePods, err := c.listAgentPodsOnNode(ctx, targetNode)
		if err == nil && len(nodePods) > 0 {
			return nodePods[0], nil
		}
	}
	return pods[0], nil
}

// listAgentPods 列出选择器命中的全部 agent Pod。
func (c *BladeCloudExec) listAgentPods(ctx context.Context) ([]string, error) {
	out, err := c.runKubectl(ctx, []string{
		"get", "pods",
		"-n", c.cfg.AgentNamespace,
		"-l", c.cfg.AgentPodSelector,
		"-o", "jsonpath={.items[*].metadata.name}",
	})
	if err != nil {
		return nil, err
	}
	return splitFields(out), nil
}

// listAgentPodsOnNode 列出指定节点上的 agent Pod（节点归属只能用 field-selector）。
func (c *BladeCloudExec) listAgentPodsOnNode(ctx context.Context, node string) ([]string, error) {
	out, err := c.runKubectl(ctx, []string{
		"get", "pods",
		"-n", c.cfg.AgentNamespace,
		"-l", c.cfg.AgentPodSelector,
		"--field-selector", "spec.nodeName=" + node,
		"-o", "jsonpath={.items[*].metadata.name}",
	})
	if err != nil {
		return nil, err
	}
	return splitFields(out), nil
}

// runKubectl 执行 kubectl 并返回去空白的 stdout；非零退出码包中文错误。
func (c *BladeCloudExec) runKubectl(ctx context.Context, subArgs []string) (string, error) {
	fullArgs := append(c.globalArgs(), subArgs...)
	cmd := exec.CommandContext(ctx, c.cfg.KubectlPath, fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("kubectl 执行被取消: %w", ctx.Err())
		}
		return "", fmt.Errorf("kubectl %s 执行失败: %v; stderr=%s",
			strings.Join(subArgs, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// globalArgs kubectl 全局参数（--kubeconfig 必须放在子命令前）。
func (c *BladeCloudExec) globalArgs() []string {
	if c.cfg.Kubeconfig == "" {
		return nil
	}
	return []string{"--kubeconfig", c.cfg.Kubeconfig}
}

// splitFields 按空白切分 jsonpath 的 `{.items[*].metadata.name}` 输出。
func splitFields(s string) []string {
	fields := strings.Fields(strings.TrimSpace(s))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
