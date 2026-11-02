// desktop_targets.go 桌面服务器目标查询（桌面化第一批）。
//
// 一次实训对应一个独立 namespace（model.NamespaceName(sessionID)），
// namespace 内每个 Pod 即桌面上一个“服务器”：返回名称、状态与可切换的容器
// 列表，作为桌面图标与 exec 路由的唯一数据源。
//
// 列表直接读 K8s API（真实集群）或 fake clientset（本地降级）。
package service

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/quenching/sandbox-service/internal/model"
)

const (
	// labelComponentKey/Value 标识平台内部组件 Pod（如行为模拟器
	// behavior-runner）。这类 Pod 不是供用户连接的业务服务器，
	// 不应出现在桌面入口。
	labelComponentKey = "quenching.io/component"
	componentBehavior = "behavior-runner"
	// labelDisplayName Pod 上声明的人类可读名（由各沙箱 Chart 打标），
	// 桌面入口优先展示它，避免直接暴露带哈希的 Pod 名。
	labelDisplayName = "quenching.io/display-name"
)

// ContainerTarget Pod 内单个容器（窗口内可切换的 exec 目标）。
type ContainerTarget struct {
	Name          string `json:"name"`
	WorkingDir    string `json:"working_dir,omitempty"`
	DeploymentDir string `json:"deployment_dir,omitempty"`
	ConfigDir     string `json:"config_dir,omitempty"`
}

// ServerTarget 桌面上一个服务器（对应一个 Pod）。
type ServerTarget struct {
	Workload     string `json:"workload"`
	TemplateSlug string `json:"template_slug"`
	// Name Pod 名，也是终端连接的 target 参数。
	Name string `json:"name"`
	// DisplayName 去掉 Deployment Pod 哈希后缀的可读名，供桌面展示。
	DisplayName string `json:"displayName"`
	// Phase Pod 阶段：Pending/Running/Succeeded/Failed。
	Phase string `json:"phase"`
	// Ready 是否可连（至少一个容器就绪）。
	Ready bool `json:"ready"`
	// Containers 该 Pod 内可 exec 的容器列表。
	Containers []ContainerTarget `json:"containers"`
}

// ListTargets 列出某次实训 namespace 内全部服务器目标，按名称升序。
func (c *K8sClient) ListTargets(
	ctx context.Context, sessionID int64,
) ([]ServerTarget, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}
	namespace := model.NamespaceName(sessionID)
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("列出沙箱 Pod: %w", err)
	}

	targets := make([]ServerTarget, 0, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]

		// 平台内部组件（行为模拟器等）不是业务服务器，不进入桌面入口。
		if pod.Labels[labelComponentKey] == componentBehavior {
			continue
		}

		containers := make([]ContainerTarget, 0, len(pod.Spec.Containers))
		for j := range pod.Spec.Containers {
			target, err := containerDirectories(pod, pod.Spec.Containers[j])
			if err != nil {
				return nil, err
			}
			containers = append(containers, target)
		}

		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
				break
			}
		}

		targets = append(targets, ServerTarget{
			Workload:     pod.Labels["app.kubernetes.io/component"],
			TemplateSlug: pod.Labels["app.kubernetes.io/instance"],
			Name:         pod.Name,
			DisplayName:  displayName(pod),
			Phase:        string(pod.Status.Phase),
			Ready:        ready,
			Containers:   containers,
		})
	}
	return targets, nil
}

// displayName 取 Pod 的可读名：优先用 Chart 打的 display-name 标签，
// 缺省时退化为去掉哈希后缀的 Pod 名。
func displayName(pod *corev1.Pod) string {
	if v := strings.TrimSpace(pod.Labels[labelDisplayName]); v != "" {
		return v
	}
	return friendlyPodName(pod.Name)
}

// friendlyPodName 去掉 Deployment Pod 名末尾的两段哈希后缀
// （ReplicaSet 哈希 + Pod 模板哈希，如 `-7b9d8f6c7f-x9q2k`），
// 还原为 Deployment 名，便于用户识别。不像哈希的后缀原样保留。
func friendlyPodName(podName string) string {
	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		return podName
	}
	last, prev := parts[len(parts)-1], parts[len(parts)-2]
	if isHashToken(last) && isHashToken(prev) {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return podName
}

// isHashToken 判断是否为 K8s 生成的哈希段：纯小写字母数字、长度 4-10。
func isHashToken(s string) bool {
	if len(s) < 4 || len(s) > 10 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
