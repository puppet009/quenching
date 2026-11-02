// behavior_test.go 行为模拟运行时与工作负载清单测试（行为模拟第一批）。
package tests

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// newBehaviorK8sClient 构造带 fake clientset 的 K8sClient（restCfg 为 nil，
// 与生产 fake 降级一致，RealCluster 返回 false）。
func newBehaviorK8sClient() *service.K8sClient {
	return service.NewK8sClient(fake.NewSimpleClientset())
}

// TestApplyBehaviorWorkload 验证 fake clientset 下清单生成幂等：
// 连续 Apply 两次应得到同一 Deployment，且规格字段符合预期。
func TestApplyBehaviorWorkload(t *testing.T) {
	kc := newBehaviorK8sClient()
	ctx := context.Background()
	spec := service.BehaviorWorkloadSpec{
		SessionID:      42,
		EntryURL:       "http://frontend:8080",
		SignalEndpoint: "http://sandbox-service:8089/api/v1/internal/behavior/signals",
	}

	uid1, err := kc.ApplyBehaviorWorkload(ctx, spec)
	if err != nil {
		t.Fatalf("首次 Apply 失败: %v", err)
	}
	// 注：fake clientset 不会像真实 API Server 那样填充 UID（uid1 可能为空），
	// 因此这里不校验非空；真实集群上 UID 由 API Server 保证。

	uid2, err := kc.ApplyBehaviorWorkload(ctx, spec)
	if err != nil {
		t.Fatalf("二次 Apply（应幂等更新）失败: %v", err)
	}
	if uid2 != uid1 {
		t.Fatalf("幂等更新后 UID 应保持不变：首次 %q，二次 %q", uid1, uid2)
	}

	dep, err := kc.GetBehaviorWorkload(ctx, 42)
	if err != nil {
		t.Fatalf("查询工作负载失败: %v", err)
	}
	if dep == nil {
		t.Fatal("工作负载应存在")
	}
	if dep.Namespace != model.NamespaceName(42) {
		t.Fatalf("namespace 不符：%s", dep.Namespace)
	}
	if dep.Labels["quenching.io/session"] != "42" {
		t.Fatalf("session 标签不符：%s", dep.Labels["quenching.io/session"])
	}
	if *dep.Spec.Replicas != 1 {
		t.Fatalf("默认副本数应为 1，实际 %d", *dep.Spec.Replicas)
	}
	foundEntry := false
	for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "ENTRY_URL" && env.Value == "http://frontend:8080" {
			foundEntry = true
		}
	}
	if !foundEntry {
		t.Fatal("ENTRY_URL 环境变量缺失或值不符")
	}
}

// TestDeleteBehaviorWorkload 验证删除幂等：删除后 Get 返回 nil，
// 再删一次也不报错。
func TestDeleteBehaviorWorkload(t *testing.T) {
	kc := newBehaviorK8sClient()
	ctx := context.Background()

	if _, err := kc.ApplyBehaviorWorkload(ctx, service.BehaviorWorkloadSpec{SessionID: 7}); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if err := kc.DeleteBehaviorWorkload(ctx, 7); err != nil {
		t.Fatalf("首次删除失败: %v", err)
	}
	dep, err := kc.GetBehaviorWorkload(ctx, 7)
	if err != nil {
		t.Fatalf("删除后查询失败: %v", err)
	}
	if dep != nil {
		t.Fatal("删除后工作负载应为 nil")
	}
	if err := kc.DeleteBehaviorWorkload(ctx, 7); err != nil {
		t.Fatalf("删除不存在的工作负载应幂等成功，实际: %v", err)
	}
}

// TestBehaviorRuntimeFakeDegraded 验证本地降级语义：
// fake clientset 下 Start 不部署工作负载、返回 degraded，且无任何假信号产生。
func TestBehaviorRuntimeFakeDegraded(t *testing.T) {
	kc := newBehaviorK8sClient()
	rt := service.NewBehaviorRuntime(kc)
	ctx := context.Background()

	st, err := rt.Start(ctx, service.BehaviorStartConfig{
		SessionID: 99,
		EntryURL:  "http://frontend:8080",
	})
	if err != nil {
		t.Fatalf("fake 环境 Start 不应返回错误: %v", err)
	}
	if st.Phase != service.BehaviorPhaseDegraded {
		t.Fatalf("fake 环境应为 degraded，实际 %s", st.Phase)
	}
	if st.Reason == "" {
		t.Fatal("degraded 状态应给出原因")
	}

	// 降级绝不触达集群：工作负载必须不存在。
	dep, err := kc.GetBehaviorWorkload(ctx, 99)
	if err != nil {
		t.Fatalf("查询工作负载失败: %v", err)
	}
	if dep != nil {
		t.Fatal("fake 降级不得创建任何工作负载")
	}

	// Status 与 Start 返回口径一致。
	st2, err := rt.Status(ctx, 99)
	if err != nil {
		t.Fatalf("Status 查询失败: %v", err)
	}
	if st2.Phase != service.BehaviorPhaseDegraded {
		t.Fatalf("Status 应为 degraded，实际 %s", st2.Phase)
	}
}

// TestBehaviorRuntimeStopIdempotent 验证降级环境下 Stop 幂等，
// 停止后状态回到 stopped（内存标记已清）。
func TestBehaviorRuntimeStopIdempotent(t *testing.T) {
	kc := newBehaviorK8sClient()
	rt := service.NewBehaviorRuntime(kc)
	ctx := context.Background()

	if _, err := rt.Start(ctx, service.BehaviorStartConfig{SessionID: 5}); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if err := rt.Stop(ctx, 5); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	if err := rt.Stop(ctx, 5); err != nil {
		t.Fatalf("重复 Stop 应幂等成功: %v", err)
	}
	st, err := rt.Status(ctx, 5)
	if err != nil {
		t.Fatalf("Stop 后 Status 查询失败: %v", err)
	}
	if st.Phase != service.BehaviorPhaseStopped {
		t.Fatalf("Stop 后应为 stopped，实际 %s", st.Phase)
	}
}

// TestBehaviorInvalidSession 验证非法会话ID被拒绝。
func TestBehaviorInvalidSession(t *testing.T) {
	kc := newBehaviorK8sClient()
	rt := service.NewBehaviorRuntime(kc)
	ctx := context.Background()

	if _, err := rt.Start(ctx, service.BehaviorStartConfig{SessionID: 0}); err == nil {
		t.Fatal("SessionID=0 应被拒绝")
	}
	if err := rt.Stop(ctx, -1); err == nil {
		t.Fatal("Stop 负数 SessionID 应被拒绝")
	}
	if _, err := rt.Status(ctx, 0); err == nil {
		t.Fatal("Status SessionID=0 应被拒绝")
	}
}
