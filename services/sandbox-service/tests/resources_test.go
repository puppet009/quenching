package tests

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/quenching/sandbox-service/internal/service"
)

// makeReadyNode 构造一个 Ready 状态的节点，指定可分配资源量。
func makeReadyNode(name string, cpuCores, memoryGi int) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(itoa(cpuCores)),
				corev1.ResourceMemory: resource.MustParse(itoa(memoryGi) + "Gi"),
			},
		},
	}
}

// makePod 构造一个带资源请求的 Pod。
func makePod(namespace, name string, cpuCores, memoryGi float64, phase corev1.PodPhase) *corev1.Pod {
	cpuStr := itoa(int(cpuCores*1000)) + "m"
	memStr := itoa(int(memoryGi*1024)) + "Mi"
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpuStr),
							corev1.ResourceMemory: resource.MustParse(memStr),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// itoa 简易整数转字符串，避免引入 strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestGetClusterAllocatable_Enough 资源充足时应返回 HasEnough=true。
func TestGetClusterAllocatable_Enough(t *testing.T) {
	node := makeReadyNode("node-1", 8, 32) // 8核 32G
	pod := makePod("default", "app-1", 1, 4, corev1.PodRunning)
	cs := fake.NewSimpleClientset(node, pod)
	client := service.NewK8sClient(cs)

	res, err := client.GetClusterAllocatable(context.Background())
	if err != nil {
		t.Fatalf("获取集群资源失败: %v", err)
	}
	if res.NodeCount != 1 {
		t.Errorf("节点数应为 1, got %d", res.NodeCount)
	}
	if !res.HasEnough() {
		t.Error("资源应足够创建沙箱（剩余 7核 28G），实际判断为不足")
	}
}

// TestGetClusterAllocatable_NotEnoughCPU CPU 不足时应返回 HasEnough=false。
func TestGetClusterAllocatable_NotEnoughCPU(t *testing.T) {
	// 节点总共 2 核，已用 1 核，剩余 1 核 < 沙箱需要的 2 核
	node := makeReadyNode("node-1", 2, 32)
	pod := makePod("default", "app-1", 1, 4, corev1.PodRunning)
	cs := fake.NewSimpleClientset(node, pod)
	client := service.NewK8sClient(cs)

	res, err := client.GetClusterAllocatable(context.Background())
	if err != nil {
		t.Fatalf("获取集群资源失败: %v", err)
	}
	if res.HasEnough() {
		t.Error("CPU 不足（剩余 1核 < 需 2核），应判断为不足")
	}
}

// TestGetClusterAllocatable_NotEnoughMemory 内存不足时应返回 HasEnough=false。
func TestGetClusterAllocatable_NotEnoughMemory(t *testing.T) {
	// 节点总共 8G，已用 5G，剩余 3G < 沙箱需要的 4G
	node := makeReadyNode("node-1", 8, 8)
	pod := makePod("default", "app-1", 1, 5, corev1.PodRunning)
	cs := fake.NewSimpleClientset(node, pod)
	client := service.NewK8sClient(cs)

	res, err := client.GetClusterAllocatable(context.Background())
	if err != nil {
		t.Fatalf("获取集群资源失败: %v", err)
	}
	if res.HasEnough() {
		t.Error("内存不足（剩余 3G < 需 4G），应判断为不足")
	}
}

// TestGetClusterAllocatable_SucceededPodNotCounted 已成功/失败的 Pod 不计入已用资源。
func TestGetClusterAllocatable_SucceededPodNotCounted(t *testing.T) {
	node := makeReadyNode("node-1", 2, 4) // 刚好够一个沙箱
	// 一个 Succeeded 状态的 Pod（已终止），其资源不应计入
	pod := makePod("default", "finished", 1, 2, corev1.PodSucceeded)
	cs := fake.NewSimpleClientset(node, pod)
	client := service.NewK8sClient(cs)

	res, err := client.GetClusterAllocatable(context.Background())
	if err != nil {
		t.Fatalf("获取集群资源失败: %v", err)
	}
	if !res.HasEnough() {
		t.Error("已终止的 Pod 不应占用资源，应刚好够创建一个沙箱")
	}
}

// TestGetClusterAllocatable_NotReadyNodeIgnored 非 Ready 状态的节点不计入。
func TestGetClusterAllocatable_NotReadyNodeIgnored(t *testing.T) {
	readyNode := makeReadyNode("ready-1", 2, 4)
	notReadyNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "not-ready-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("64Gi"),
			},
		},
	}
	cs := fake.NewSimpleClientset(readyNode, notReadyNode)
	client := service.NewK8sClient(cs)

	res, err := client.GetClusterAllocatable(context.Background())
	if err != nil {
		t.Fatalf("获取集群资源失败: %v", err)
	}
	if res.NodeCount != 1 {
		t.Errorf("只应统计 1 个 Ready 节点，实际统计了 %d 个", res.NodeCount)
	}
}

// TestClusterResourcesHasEnough_ExactlySufficient 刚好够用时应返回 true。
func TestClusterResourcesHasEnough_ExactlySufficient(t *testing.T) {
	res := &service.ClusterResources{
		AllocatableCPU:    resource.MustParse("2"),
		AllocatableMemory: resource.MustParse("4Gi"),
		NodeCount:         1,
	}
	if !res.HasEnough() {
		t.Error("刚好 2核 4G 应判断为足够")
	}
}
