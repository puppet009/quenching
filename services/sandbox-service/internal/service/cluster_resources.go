package service

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterResources 集群可分配资源总量（已减去已分配的）。
type ClusterResources struct {
	AllocatableCPU    resource.Quantity
	AllocatableMemory resource.Quantity
	NodeCount         int
}

// HasEnough 判断集群剩余资源是否足够创建一个沙箱。
// 单沙箱配额：CPU 2核、内存 4Gi（与 BuildResourceQuota 保持一致）。
func (r *ClusterResources) HasEnough() bool {
	cpuNeed := resource.MustParse("2")
	memNeed := resource.MustParse("4Gi")
	return r.AllocatableCPU.Cmp(cpuNeed) >= 0 && r.AllocatableMemory.Cmp(memNeed) >= 0
}

// GetClusterAllocatable 统计集群可分配资源总量。
// 用 Allocatable（节点可分配）减去已请求的资源，得到剩余可用量。
//
// 注意：这是一个粗略估算，用于提前拦截明显资源不足的场景，
// 真正的调度由 kube-scheduler 决定。如果检查失败直接返回友好错误，
// 避免 Pod 一直处于 Pending 状态浪费用户等待时间。
func (c *K8sClient) GetClusterAllocatable(ctx context.Context) (*ClusterResources, error) {
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取节点列表失败: %w", err)
	}

	var totalCPU, totalMem resource.Quantity
	readyCount := 0

	for _, node := range nodes.Items {
		// 只统计 Ready 状态的节点
		if !nodeReady(&node) {
			continue
		}
		readyCount++
		totalCPU.Add(node.Status.Allocatable[corev1.ResourceCPU])
		totalMem.Add(node.Status.Allocatable[corev1.ResourceMemory])
	}

	if readyCount == 0 {
		return nil, fmt.Errorf("集群中没有 Ready 状态的节点")
	}

	// 统计所有 namespace 中已请求的资源
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取 Pod 列表失败: %w", err)
	}

	var usedCPU, usedMem resource.Quantity
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue // 已终止的 Pod 不占资源
		}
		for _, container := range pod.Spec.Containers {
			if cpuReq, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				usedCPU.Add(cpuReq)
			}
			if memReq, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				usedMem.Add(memReq)
			}
		}
	}

	// 可分配 = 总量 - 已使用
	totalCPU.Sub(usedCPU)
	totalMem.Sub(usedMem)

	return &ClusterResources{
		AllocatableCPU:    totalCPU,
		AllocatableMemory: totalMem,
		NodeCount:         readyCount,
	}, nil
}

// nodeReady 检查节点是否处于 Ready 状态。
func nodeReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}
