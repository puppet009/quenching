// Package tests 沙箱服务单测。
// 用 fake clientset 在本机（无需真实集群）验证沙箱创建/幂等/删除/调谐逻辑。
// 运行：go test ./... -race
package tests

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// TestEnsureSandboxResourcesCreatesAll 验证创建沙箱时 ns/quota/networkpolicy 三件套都建出来。
func TestEnsureSandboxResourcesCreatesAll(t *testing.T) {
	cs := fake.NewSimpleClientset()
	client := service.NewK8sClient(cs)
	ctx := context.Background()

	ns, err := client.EnsureSandboxResources(ctx, "1001", "2001")
	if err != nil {
		t.Fatalf("创建沙箱资源失败: %v", err)
	}
	if ns != "sandbox-1001" {
		t.Fatalf("namespace 名称错误, got %s", ns)
	}

	// namespace 存在且带正确标签
	got, err := cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace 未创建: %v", err)
	}
	if got.Labels[service.LabelSession] != "1001" {
		t.Errorf("session 标签错误: %v", got.Labels)
	}

	// ResourceQuota 存在
	if _, err := cs.CoreV1().ResourceQuotas(ns).Get(ctx, "sandbox-quota", metav1.GetOptions{}); err != nil {
		t.Errorf("ResourceQuota 未创建: %v", err)
	}

	// NetworkPolicy 存在
	if _, err := cs.NetworkingV1().NetworkPolicies(ns).Get(ctx, "sandbox-isolation", metav1.GetOptions{}); err != nil {
		t.Errorf("NetworkPolicy 未创建: %v", err)
	}
}

// TestEnsureSandboxResourcesIdempotent 验证重复创建不报错（调谐器会反复调用，必须幂等）。
func TestEnsureSandboxResourcesIdempotent(t *testing.T) {
	cs := fake.NewSimpleClientset()
	client := service.NewK8sClient(cs)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := client.EnsureSandboxResources(ctx, "1002", "2002"); err != nil {
			t.Fatalf("第 %d 次幂等创建失败: %v", i+1, err)
		}
	}
}

// TestDeleteSandbox 验证删除沙箱后 namespace 被清理。
func TestDeleteSandbox(t *testing.T) {
	ns := service.BuildNamespace("1003", "2003")
	cs := fake.NewSimpleClientset(ns)
	client := service.NewK8sClient(cs)
	ctx := context.Background()

	if err := client.DeleteSandbox(ctx, "sandbox-1003"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := cs.CoreV1().Namespaces().Get(ctx, "sandbox-1003", metav1.GetOptions{}); err == nil {
		t.Fatal("namespace 删除后仍存在，会导致资源泄漏")
	}
}

// TestReconcileReclaimsOrphan 验证调谐器能回收"集群里有、DB 已无记录"的孤儿 namespace。
func TestReconcileReclaimsOrphan(t *testing.T) {
	// 集群里有一个沙箱 ns，但 DB(Store) 里没有任何 active 沙箱 -> 应被回收
	orphan := service.BuildNamespace("9999", "2004")
	cs := fake.NewSimpleClientset(orphan)
	client := service.NewK8sClient(cs)

	rec := newTestReconciler(client, &fakeStore{}) // Store 返回空 active 列表
	if err := rec.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("调谐失败: %v", err)
	}

	nsList, _ := client.ListSandboxNamespaces(context.Background())
	if len(nsList) != 0 {
		t.Fatalf("孤儿 namespace 未回收，剩余: %v", nsList)
	}
}

// TestStateMachine 验证状态机只允许合法流转。
func TestStateMachine(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{model.StatusCreating, model.StatusDeploying, true},
		{model.StatusCreating, model.StatusRunning, false}, // 不能跳级
		{model.StatusRunning, model.StatusReleasing, true},
		{model.StatusReleasing, model.StatusReleased, true},
		{model.StatusReleased, model.StatusRunning, false}, // 终态不可回
		{model.StatusFailed, model.StatusReleasing, true},  // 失败也要回收
	}
	for _, c := range cases {
		if got := model.CanTransition(c.from, c.to); got != c.want {
			t.Errorf("CanTransition(%s->%s)=%v, want %v", c.from, c.to, got, c.want)
		}
	}
}

// 引用未使用的类型，保证 import 不被删（fake 场景下 corev1/netv1 用于可读性）。
var (
	_ = corev1.Namespace{}
	_ = netv1.NetworkPolicy{}
)
