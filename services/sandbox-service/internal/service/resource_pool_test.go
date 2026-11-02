package service

import (
	"context"
	"github.com/quenching/sandbox-service/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func poolNode(name string) *core.Node {
	return &core.Node{ObjectMeta: meta.ObjectMeta{Name: name}, Status: core.NodeStatus{Allocatable: core.ResourceList{core.ResourceCPU: resource.MustParse("4"), core.ResourceMemory: resource.MustParse("8Gi")}, Conditions: []core.NodeCondition{{Type: core.NodeReady, Status: core.ConditionTrue}}}}
}
func TestPoolCountsPlatformAndInit(t *testing.T) {
	n := poolNode("one")
	p := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "db", Namespace: "platform"}, Spec: core.PodSpec{NodeName: "one", Containers: []core.Container{{Name: "db", Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("1Gi")}}}}, InitContainers: []core.Container{{Name: "init", Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("2"), core.ResourceMemory: resource.MustParse("256Mi")}}}}, Overhead: core.ResourceList{core.ResourceCPU: resource.MustParse("100m")}}, Status: core.PodStatus{Phase: core.PodRunning}}
	client := fake.NewSimpleClientset(n, p)
	pool := ResourcePool{Client: client, PlatformNamespace: "platform"}
	s, e := pool.Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if s.Nodes[0].Requested.CPUMilli != 2100 || s.Groups["platform"].Requested.MemoryBytes != 1073741824 || s.Nodes[0].Usage != nil {
		t.Fatal(s)
	}
	if s.Pods[0].Limits.CPUMilli != 100 || s.Pods[0].Limits.MemoryBytes != 0 {
		t.Fatal("allocation limits must use actual pod resources, including overhead")
	}
	n2 := poolNode("two")
	_, _ = client.CoreV1().Nodes().Create(context.Background(), n2, meta.CreateOptions{})
	s, _ = pool.Snapshot(context.Background())
	if len(s.Nodes) != 2 || s.Available.CPUMilli != 5900 {
		t.Fatal(s)
	}
}
func TestPoolUnavailableAndReservations(t *testing.T) {
	a := poolNode("a")
	a.Spec.Unschedulable = true
	b := poolNode("b")
	b.Spec.Taints = []core.Taint{{Key: "dedicated", Effect: core.TaintEffectNoSchedule}}
	pool := ResourcePool{Client: fake.NewSimpleClientset(a, b)}
	s, _ := pool.Snapshot(context.Background())
	if s.Available.CPUMilli != 0 {
		t.Fatal(s)
	}
	pool.Client = fake.NewSimpleClientset(poolNode("ready"))
	s, _ = pool.Snapshot(context.Background())
	p := model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 3000, MemoryBytes: 1073741824}, Components: []model.ResourceComponent{{Name: "large", Requests: model.ResourceAmounts{CPUMilli: 3000, MemoryBytes: 1073741824}}}}
	reserved := []model.Sandbox{{Namespace: "sandbox-1", ResourcesAdmitted: true, ResourcePlan: p}}
	if ok, _ := CanAdmit(s, p, reserved); ok {
		t.Fatal("unobserved reservation sold twice")
	}
	if ok, _ := CanAdmit(s, p, nil); !ok {
		t.Fatal("capacity rejected")
	}
}
