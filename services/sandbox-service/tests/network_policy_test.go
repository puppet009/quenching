package tests

import (
	"testing"

	netv1 "k8s.io/api/networking/v1"

	"github.com/quenching/sandbox-service/internal/service"
)

// TestNetworkPolicy_DefaultDenyAll 验证 NetworkPolicy 默认拒绝所有入站/出站。
// 关键：PolicyTypes 同时包含 Ingress 和 Egress，且没有默认放行的通配规则。
func TestNetworkPolicy_DefaultDenyAll(t *testing.T) {
	policy := service.BuildNetworkPolicy("sandbox-1001")

	// 必须同时管控入站和出站
	hasIngress := false
	hasEgress := false
	for _, pt := range policy.Spec.PolicyTypes {
		if pt == netv1.PolicyTypeIngress {
			hasIngress = true
		}
		if pt == netv1.PolicyTypeEgress {
			hasEgress = true
		}
	}
	if !hasIngress || !hasEgress {
		t.Errorf("NetworkPolicy 必须同时管控 Ingress 和 Egress, got PolicyTypes=%v", policy.Spec.PolicyTypes)
	}

	// PodSelector 为空 = 作用于 namespace 内所有 Pod
	if policy.Spec.PodSelector.Size() != 0 {
		t.Errorf("PodSelector 应为空（作用于所有 Pod），实际有 %d 个 label", policy.Spec.PodSelector.Size())
	}
}

// TestNetworkPolicy_IngressOnlyPlatformAndIntraNs 验证入站规则：只放行平台 + 沙箱内。
// 沙箱间（其他 sandbox namespace）的入站应被默认拒绝。
func TestNetworkPolicy_IngressOnlyPlatformAndIntraNs(t *testing.T) {
	policy := service.BuildNetworkPolicy("sandbox-1001")

	if len(policy.Spec.Ingress) != 2 {
		t.Fatalf("入站规则应有 2 条（平台 + 沙箱内），实际 %d 条", len(policy.Spec.Ingress))
	}

	// 规则 1：平台 namespace 访问
	ingressPlatform := policy.Spec.Ingress[0]
	if len(ingressPlatform.From) != 1 {
		t.Errorf("平台入站规则应有 1 个来源，实际 %d 个", len(ingressPlatform.From))
	} else {
		peer := ingressPlatform.From[0]
		if peer.NamespaceSelector == nil {
			t.Fatal("平台入站规则应使用 NamespaceSelector")
		}
		roleLabel, ok := peer.NamespaceSelector.MatchLabels[service.LabelRole]
		if !ok || roleLabel != "platform" {
			t.Errorf("平台入站应通过 role=platform 标签选择，实际 labels=%v", peer.NamespaceSelector.MatchLabels)
		}
	}

	// 规则 2：沙箱内 Pod 互访
	ingressIntra := policy.Spec.Ingress[1]
	if len(ingressIntra.From) != 1 {
		t.Errorf("沙箱内入站规则应有 1 个来源，实际 %d 个", len(ingressIntra.From))
	} else {
		peer := ingressIntra.From[0]
		if peer.PodSelector == nil {
			t.Fatal("沙箱内入站规则应使用 PodSelector")
		}
		// 空 PodSelector = 同 namespace 内所有 Pod
		if peer.PodSelector.Size() != 0 {
			t.Errorf("沙箱内 PodSelector 应为空（匹配所有 Pod），实际有 %d 个 label", peer.PodSelector.Size())
		}
	}
}

// TestNetworkPolicy_EgressRules 验证出站规则：沙箱内 + DNS + 平台。
// 出站到其他沙箱 namespace 应被默认拒绝。
func TestNetworkPolicy_EgressRules(t *testing.T) {
	policy := service.BuildNetworkPolicy("sandbox-1001")

	if len(policy.Spec.Egress) != 3 {
		t.Fatalf("出站规则应有 3 条（沙箱内 + DNS + 平台），实际 %d 条", len(policy.Spec.Egress))
	}

	// 规则 1：沙箱内 Pod 互访
	egressIntra := policy.Spec.Egress[0]
	if len(egressIntra.To) != 1 || egressIntra.To[0].PodSelector == nil {
		t.Error("出站规则 1 应为沙箱内 Pod 互访（PodSelector）")
	}

	// 规则 2：DNS（kube-system 的 53 端口）
	egressDNS := policy.Spec.Egress[1]
	if len(egressDNS.To) != 1 {
		t.Errorf("DNS 出站规则应有 1 个目标，实际 %d 个", len(egressDNS.To))
	} else {
		peer := egressDNS.To[0]
		if peer.NamespaceSelector == nil {
			t.Fatal("DNS 出站应使用 NamespaceSelector")
		}
		nsLabel, ok := peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
		if !ok || nsLabel != "kube-system" {
			t.Errorf("DNS 出站应指向 kube-system，实际 labels=%v", peer.NamespaceSelector.MatchLabels)
		}
	}
	// 验证 DNS 端口限制
	if len(egressDNS.Ports) != 2 {
		t.Errorf("DNS 应限制 TCP+UDP 53 端口，实际 %d 个端口规则", len(egressDNS.Ports))
	}

	// 规则 3：平台 namespace
	egressPlatform := policy.Spec.Egress[2]
	if len(egressPlatform.To) != 1 {
		t.Errorf("平台出站规则应有 1 个目标，实际 %d 个", len(egressPlatform.To))
	} else {
		peer := egressPlatform.To[0]
		if peer.NamespaceSelector == nil {
			t.Fatal("平台出站规则应使用 NamespaceSelector")
		}
		roleLabel, ok := peer.NamespaceSelector.MatchLabels[service.LabelRole]
		if !ok || roleLabel != "platform" {
			t.Errorf("平台出站应通过 role=platform 标签选择，实际 labels=%v", peer.NamespaceSelector.MatchLabels)
		}
	}
}

// TestNetworkPolicy_TwoSandboxesIsolated 验证两个沙箱的 NetworkPolicy 互不放行。
// 完成标准：两沙箱间网络不通。
func TestNetworkPolicy_TwoSandboxesIsolated(t *testing.T) {
	policyA := service.BuildNetworkPolicy("sandbox-1001")
	policyB := service.BuildNetworkPolicy("sandbox-1002")

	// 两个沙箱的入站规则都不应该放行对方的 namespace
	checkNoCrossSandboxIngress(t, policyA, "sandbox-1001")
	checkNoCrossSandboxIngress(t, policyB, "sandbox-1002")

	// 两个沙箱的出站规则都不应该指向其他沙箱 namespace
	checkNoCrossSandboxEgress(t, policyA)
	checkNoCrossSandboxEgress(t, policyB)
}

// checkNoCrossSandboxIngress 检查入站规则中没有放行其他 sandbox namespace 的规则。
// 只有 role=platform 的 namespace 才能入站，其他 sandbox ns 都被默认拒绝。
func checkNoCrossSandboxIngress(t *testing.T, policy *netv1.NetworkPolicy, ns string) {
	t.Helper()
	for i, rule := range policy.Spec.Ingress {
		for j, peer := range rule.From {
			if peer.NamespaceSelector != nil {
				// 只允许 role=platform 的 namespace 入站
				role := peer.NamespaceSelector.MatchLabels[service.LabelRole]
				if role != "platform" {
					t.Errorf("沙箱 %s 的入站规则[%d][%d]放行的 namespace 不是 platform，role=%s",
						ns, i, j, role)
				}
			}
		}
	}
}

// checkNoCrossSandboxEgress 检查出站规则中没有指向其他 sandbox namespace 的规则。
func checkNoCrossSandboxEgress(t *testing.T, policy *netv1.NetworkPolicy) {
	t.Helper()
	for i, rule := range policy.Spec.Egress {
		for j, peer := range rule.To {
			if peer.NamespaceSelector != nil {
				labels := peer.NamespaceSelector.MatchLabels
				// 只允许 kube-system(DNS) 和 platform
				isKubeDNS := labels["kubernetes.io/metadata.name"] == "kube-system"
				isPlatform := labels[service.LabelRole] == "platform"
				if !isKubeDNS && !isPlatform {
					t.Errorf("出站规则[%d][%d]指向了不允许的 namespace: %v", i, j, labels)
				}
			}
		}
	}
}
