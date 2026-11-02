// exec_runner.go 非交互式命令执行底座（文件管理器第二批）。
//
// 与 ExecShell（交互 TTY）不同，这里执行给定命令并收集 stdout，stderr 非空
// 或命令非零退出时返回错误。文件管理器的所有操作（ls/stat/mkdir/mv/cp/...）
// 都经此通道在真实容器内执行，不向 Pod 安装任何 agent。
package service

import (
	"bytes"
	"context"
	"fmt"
	"io"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/quenching/sandbox-service/internal/model"
)

// execOptions 控制一次非交互执行。
type execOptions struct {
	sessionID int64
	pod       string
	container string
	command   []string
	stdin     io.Reader
	stdout    io.Writer // 为空时收集到返回值
}

// runExec 在容器内执行命令；stdout 为空时返回收集到的标准输出。
func (c *K8sClient) runExec(ctx context.Context, o execOptions) ([]byte, error) {
	if o.sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}
	if o.pod == "" {
		return nil, fmt.Errorf("缺少目标服务器")
	}
	if len(o.command) == 0 {
		return nil, fmt.Errorf("缺少待执行命令")
	}
	if c.restCfg == nil {
		return nil, fmt.Errorf("文件操作需要 Kubernetes 环境，当前为本地 fake 降级")
	}

	namespace := model.NamespaceName(o.sessionID)
	container := o.container
	if container == "" {
		container, _ = c.firstContainer(ctx, namespace, o.pod)
		if container == "" {
			return nil, fmt.Errorf("服务器 %s 没有可连接的容器", o.pod)
		}
	}

	executor, err := remotecommand.NewSPDYExecutor(c.restCfg, "POST", c.clientset.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("pods").
		Name(o.pod).
		SubResource("exec").
		VersionedParams(
			&coreV1.PodExecOptions{
				Container: container,
				Command:   o.command,
				Stdin:     o.stdin != nil,
				Stdout:    true,
				Stderr:    true,
				TTY:       false,
			},
			scheme.ParameterCodec,
		).URL())
	if err != nil {
		return nil, fmt.Errorf("建立 exec 通道失败: %w", err)
	}

	var captured bytes.Buffer
	stdout := o.stdout
	if stdout == nil {
		stdout = &captured
	}
	var stderr bytes.Buffer
	streamOpts := remotecommand.StreamOptions{
		Stdin:  o.stdin,
		Stdout: stdout,
		Stderr: &stderr,
		Tty:    false,
	}
	if err := executor.StreamWithContext(ctx, streamOpts); err != nil {
		if stderr.Len() > 0 {
			return captured.Bytes(), fmt.Errorf("%s", stderr.String())
		}
		return captured.Bytes(), fmt.Errorf("exec 失败: %w", err)
	}
	return captured.Bytes(), nil
}

// ExecCommand 供内部只读接口（handler 包）执行已校验的命令并收集输出。
// 命令的白名单校验由调用方（internal_readonly）负责，本方法只负责在目标
// Pod/容器内执行。t 提供会话与 Pod/容器定位。
func (c *K8sClient) ExecCommand(ctx context.Context, t FileTarget, command []string) ([]byte, error) {
	return c.runExec(ctx, execOptions{
		sessionID: t.sessionID,
		pod:       t.pod,
		container: t.container,
		command:   command,
	})
}
