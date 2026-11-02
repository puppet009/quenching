package service

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ReadyWaiter 等待沙箱内资源就绪。
// 检查指定 namespace 下的 Deployment 是否全部 Ready。
type ReadyWaiter struct {
	clientset kubernetes.Interface
	Interval  time.Duration // 轮询间隔，默认 5s
	Timeout   time.Duration // 总超时，默认 5min
}

// NewReadyWaiter 创建就绪等待器。
func NewReadyWaiter(cs kubernetes.Interface) *ReadyWaiter {
	return &ReadyWaiter{
		clientset: cs,
		Interval:  5 * time.Second,
		Timeout:   5 * time.Minute,
	}
}

// WaitForDeployments 等待 namespace 下所有 Deployment 就绪。
// 就绪条件：spec.replicas == status.readyReplicas
// 返回就绪的 Deployment 数量，超时有错。
func (w *ReadyWaiter) WaitForDeployments(ctx context.Context, namespace string) (int, error) {
	deadline := time.Now().Add(w.Timeout)
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		ready, total, err := w.checkDeployments(ctx, namespace)
		if err != nil {
			// 检查出错不直接返回（可能是临时网络波动），等下一轮
			// 但如果超时了就返回
			if time.Now().After(deadline) {
				return ready, fmt.Errorf("等待就绪超时，最后一次错误: %w", err)
			}
		} else if ready == total && total > 0 {
			// 全部就绪
			return ready, nil
		}

		select {
		case <-ctx.Done():
			return ready, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return ready, fmt.Errorf("等待 Deployment 就绪超时：%d/%d ready", ready, total)
			}
		}
	}
}

// checkDeployments 检查 namespace 下 Deployment 就绪情况。
// 返回 (就绪数, 总数, 错误)
func (w *ReadyWaiter) checkDeployments(ctx context.Context, namespace string) (int, int, error) {
	deps, err := w.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("获取 Deployment 列表失败: %w", err)
	}

	total := len(deps.Items)
	ready := 0
	for _, dep := range deps.Items {
		replicas := int32(1)
		if dep.Spec.Replicas != nil {
			replicas = *dep.Spec.Replicas
		}
		if dep.Status.ReadyReplicas >= replicas && replicas > 0 {
			ready++
		}
	}
	return ready, total, nil
}
