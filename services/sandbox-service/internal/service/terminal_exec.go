// terminal_exec.go 真实终端的 kubectl exec 桥接（桌面化第一批）。
//
// 浏览器 WebSocket 与 Kubernetes exec 通道在此对接：
//   - stdin：用户键入 → remotecommand；
//   - stdout/stderr：remotecommand → 浏览器；
//   - 终端尺寸：浏览器 resize 事件经 ResizeQueue 传给容器内进程。
//
// 仅在连真实集群（restCfg 非空）时可用；本地 fake 降级返回明确错误。
package service

import (
	"context"
	"fmt"
	"io"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/quenching/sandbox-service/internal/model"
)

// terminalCommand 容器内默认交互 shell。
var terminalCommand = []string{"/bin/sh", "-c", `
QUENCHING_TERMINAL_DIR=${1:-$PWD}
export QUENCHING_TERMINAL_DIR
terminal_rc_dir=$(mktemp -d /tmp/quenching-terminal.XXXXXX) || exit 1
trap 'rm -f "$terminal_rc_dir/rc"; rmdir "$terminal_rc_dir" 2>/dev/null || true' EXIT
cat > "$terminal_rc_dir/rc" <<'QUENCHING_RC'
if [ -n "${BASH_VERSION-}" ]; then
  [ ! -r /etc/bash.bashrc ] || . /etc/bash.bashrc
  [ ! -r "$HOME/.bashrc" ] || . "$HOME/.bashrc"
fi
cd -- "$QUENCHING_TERMINAL_DIR" || { printf '无法进入项目目录: %s\n' "$QUENCHING_TERMINAL_DIR" >&2; exit 1; }
__quenching_report_cwd() {
  printf '\033]7;file://localhost'
  printf '%s' "$PWD" | LC_ALL=C sed -e 's/%/%25/g' -e 's/\\/%5C/g' -e 's/ /%20/g' -e 's/#/%23/g' -e 's/?/%3F/g'
  printf '\007'
}
if [ -n "${BASH_VERSION-}" ]; then
  eval 'PROMPT_COMMAND+=(__quenching_report_cwd)'
else
  PS1='${USER:-user}@${HOSTNAME:-container}:$PWD$ $(__quenching_report_cwd)'
fi
QUENCHING_RC
if [ -x /bin/bash ]; then
  /bin/bash --noprofile --rcfile "$terminal_rc_dir/rc" -i
else
  ENV="$terminal_rc_dir/rc" /bin/sh -i
fi
`}

// ResizeQueue 终端尺寸事件队列，实现 remotecommand.TerminalSizeQueue。
type ResizeQueue struct {
	ch chan remotecommand.TerminalSize
}

// NewResizeQueue 创建尺寸队列。
func NewResizeQueue() *ResizeQueue {
	return &ResizeQueue{ch: make(chan remotecommand.TerminalSize, 8)}
}

// Push 投递一次尺寸变化（非阻塞，满则丢弃过期事件）。
func (q *ResizeQueue) Push(cols, rows uint16) {
	if q == nil {
		return
	}
	select {
	case q.ch <- remotecommand.TerminalSize{Width: cols, Height: rows}:
	default:
	}
}

// Next 阻塞等待下一个尺寸，ctx 取消时返回 nil。
func (q *ResizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.ch
	if !ok {
		return nil
	}
	return &size
}

// Close 关闭尺寸通道，让阻塞中的 Next 返回 nil。
func (q *ResizeQueue) Close() {
	if q == nil {
		return
	}
	close(q.ch)
}

// ExecShell 在指定 Pod/容器内开交互 shell 并桥接 stdin/stdout。
// container 为空时默认该 Pod 的第一个容器。
func (c *K8sClient) ExecShell(
	ctx context.Context,
	sessionID int64,
	pod, container string,
	stdin io.Reader,
	stdout io.Writer,
	sizes remotecommand.TerminalSizeQueue,
) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID非法")
	}
	if pod == "" {
		return fmt.Errorf("缺少目标服务器")
	}
	if c.restCfg == nil {
		// 本地 fake clientset 无法 exec：明确报错，前端据此提示而非白屏。
		return fmt.Errorf("真实终端需要 Kubernetes 环境，当前为本地 fake 降级（请在集群环境使用）")
	}

	namespace := model.NamespaceName(sessionID)
	p, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("读取服务器 %s: %w", pod, err)
	}
	if container == "" && len(p.Spec.Containers) > 0 {
		container = p.Spec.Containers[0].Name
	}
	var target *ContainerTarget
	for _, item := range p.Spec.Containers {
		if item.Name != container {
			continue
		}
		dir, err := containerDirectories(p, item)
		if err != nil {
			return err
		}
		target = &dir
		break
	}
	if target == nil {
		return fmt.Errorf("服务器 %s 没有可连接的容器 %s", pod, container)
	}
	command := append(append([]string{}, terminalCommand...), "quenching-terminal", target.WorkingDir)

	executor, err := remotecommand.NewSPDYExecutor(c.restCfg, "POST", c.clientset.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("pods").
		Name(pod).
		SubResource("exec").
		VersionedParams(
			&coreV1.PodExecOptions{
				Container: container,
				Command:   command,
				Stdin:     true,
				Stdout:    true,
				Stderr:    true,
				TTY:       true,
			},
			scheme.ParameterCodec,
		).URL())
	if err != nil {
		return fmt.Errorf("建立 exec 通道失败: %w", err)
	}

	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stdout, // TTY 模式合并到同一终端输出
		Tty:               true,
		TerminalSizeQueue: sizes,
	}); err != nil {
		return fmt.Errorf("exec 流失败: %w", err)
	}
	return nil
}

// firstContainer 返回 Pod 的第一个容器名；Pod 不存在时返回空串。
func (c *K8sClient) firstContainer(
	ctx context.Context, namespace, pod string,
) (string, error) {
	p, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("读取服务器 %s: %w", pod, err)
	}
	if len(p.Spec.Containers) == 0 {
		return "", nil
	}
	return p.Spec.Containers[0].Name, nil
}
