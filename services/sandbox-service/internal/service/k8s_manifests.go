// k8s_manifests.go 沙箱在 Kubernetes 中的资源定义与操作。
// 隔离三层：① Namespace（资源边界）② NetworkPolicy（网络边界）③ ResourceQuota（资源边界）。
// 所有资源用函数程序化生成（而非读 YAML 文件），便于：
//   - fake clientset 单测直接断言
//   - 将来替换为 Helm/CRD 渲染时只改这一层

package service

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	LabelSession = "quenching.io/session-id"
	LabelUser    = "quenching.io/user-id"
	LabelRole    = "quenching.io/role"
	// 平台命名空间，NetworkPolicy 放行它对沙箱的管理访问。
	PlatformNamespace = "platform"
)

// BuildNamespace 为某次实训创建独立命名空间。
// 删除沙箱 = 删除整个 namespace，里面所有资源一起清理，不会泄漏。
func BuildNamespace(sessionID, userID string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandbox-" + sessionID,
			Labels: map[string]string{
				LabelSession: sessionID,
				LabelUser:    userID,
				LabelRole:    "sandbox",
			},
		},
	}
}

// BuildResourceQuota 单沙箱资源上限：CPU 2核、内存 4Gi。
func BuildResourceQuota(namespace string) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox-quota", Namespace: namespace},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:    resource.MustParse("2"),
				corev1.ResourceLimitsMemory: resource.MustParse("4Gi"),
			},
		},
	}
}

// BuildNetworkPolicy 网络隔离：
//   - 默认拒绝沙箱 namespace 的所有入站/出站
//   - 放行：平台 namespace 访问沙箱（管理/注入/采指标/连终端）
//   - 放行：沙箱内 Pod 之间互访（微服务要互相调用）
//   - 放行：沙箱内 Pod 对 DNS 的出站
//
// 沙箱之间互不可访（不允许跨 namespace），沙箱也不能访问平台中间件。
func BuildNetworkPolicy(namespace string) *netv1.NetworkPolicy {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	return &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox-isolation", Namespace: namespace},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // 作用于 ns 内所有 Pod
			PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress, netv1.PolicyTypeEgress},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					// 平台 namespace 可访问沙箱（管理/终端/采集）。
					From: []netv1.NetworkPolicyPeer{
						{NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{LabelRole: "platform"},
						}},
					},
				},
				{
					// 沙箱内 Pod 之间互访。
					From: []netv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					},
				},
			},
			Egress: []netv1.NetworkPolicyEgressRule{
				{
					// 沙箱内 Pod 之间互访。
					To: []netv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					},
				},
				{
					// DNS 出站（kube-system 的 kube-dns）。
					To: []netv1.NetworkPolicyPeer{
						{NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
						}},
					},
					Ports: []netv1.NetworkPolicyPort{
						{Protocol: &udp, Port: intOrStr(53)},
						{Protocol: &tcp, Port: intOrStr(53)},
					},
				},
				{
					// 平台访问放通（如拉取指标回调、AI/行为模拟从平台侧发起）。
					To: []netv1.NetworkPolicyPeer{
						{NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{LabelRole: "platform"},
						}},
					},
				},
			},
		},
	}
}

func intOrStr(i int) *intstr.IntOrString {
	v := intstr.FromInt(i)
	return &v
}

// EnsureSandboxResources 幂等创建沙箱隔离资源及本班次项目操作授权。
// 反复调用结果一致——这是调谐器可安全重试的前提。
func (c *K8sClient) EnsureSandboxResources(ctx context.Context, sessionID, userID string) (string, error) {
	ns, err := c.ensureSandboxNamespace(ctx, sessionID, userID)
	if err != nil {
		return "", fmt.Errorf("创建 namespace 失败: %w", err)
	}
	if err := c.setSandboxAccess(ctx, ns.Name, false); err != nil {
		return "", fmt.Errorf("configure project access: %w", err)
	}

	if err := c.ensureFaultReader(ctx, ns.Name); err != nil {
		return "", fmt.Errorf("configure fault reader: %w", err)
	}

	quota := BuildResourceQuota(ns.Name)
	if _, err := c.clientset.CoreV1().ResourceQuotas(ns.Name).Create(ctx, quota, metav1.CreateOptions{}); err != nil {
		if !isAlreadyExists(err) {
			return "", fmt.Errorf("创建 ResourceQuota 失败: %w", err)
		}
	}

	policy := BuildNetworkPolicy(ns.Name)
	if _, err := c.clientset.NetworkingV1().NetworkPolicies(ns.Name).Create(ctx, policy, metav1.CreateOptions{}); err != nil {
		if !isAlreadyExists(err) {
			return "", fmt.Errorf("创建 NetworkPolicy 失败: %w", err)
		}
	}

	return ns.Name, nil
}

// DeleteSandbox 删除整个 namespace，释放沙箱全部资源。
func (c *K8sClient) DeleteSandbox(ctx context.Context, namespace string) error {
	if err := c.CloseSandboxAccess(ctx, namespace); err != nil {
		return err
	}
	err := c.clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("删除 namespace %s 失败: %w", namespace, err)
	}
	return nil
}

// ListSandboxNamespaces 列出集群中所有沙箱 namespace（带 role=sandbox 标签）。
// 供调谐器发现"孤儿 namespace"（DB 已无记录但集群仍在跑）并回收。
func (c *K8sClient) ListSandboxNamespaces(ctx context.Context) ([]string, error) {
	list, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: LabelRole + "=sandbox",
	})
	if err != nil {
		return nil, fmt.Errorf("列出沙箱 namespace 失败: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		names = append(names, ns.Name)
	}
	return names, nil
}
