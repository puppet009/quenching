package service

import (
	"context"
	tpl "github.com/quenching/go-common/templates"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestResourcePlanIncludesRunner(t *testing.T) {
	m := tpl.Manifest{Resources: &tpl.ResourcesSpec{CPU: "2", Memory: "4Gi", Requests: &tpl.ResourceAmounts{CPU: "350m", Memory: "1088Mi"}, Storage: "2432Mi"}, BehaviorFlow: &tpl.BehaviorFlowSpec{}}
	p, e := BuildResourcePlan(m)
	if e != nil {
		t.Fatal(e)
	}
	if p.Requests.CPUMilli != 550 || p.Requests.MemoryBytes != 1600*1024*1024 || p.Limits.CPUMilli != 3000 || p.StorageBytes != 2464*1024*1024 {
		t.Fatal(p)
	}
	kc := NewK8sClient(fake.NewSimpleClientset())
	_, e = kc.EnsureSandboxResourcesWithPlan(context.Background(), "1", "2", p)
	if e != nil {
		t.Fatal(e)
	}
	q, e := kc.Clientset().CoreV1().ResourceQuotas("sandbox-1").Get(context.Background(), "sandbox-quota", meta.GetOptions{})
	if e != nil {
		t.Fatal(e)
	}
	cpu := q.Spec.Hard[core.ResourceLimitsCPU]
	req := q.Spec.Hard[core.ResourceRequestsCPU]
	if cpu.MilliValue() != 3000 || req.MilliValue() != 550 {
		t.Fatal(q)
	}
}
func TestResourcePlanLegacyAndInvalid(t *testing.T) {
	p, e := BuildResourcePlan(tpl.Manifest{Resources: &tpl.ResourcesSpec{CPU: "1", Memory: "2Gi"}})
	if e != nil || p.Requests.CPUMilli != 1000 {
		t.Fatal(p, e)
	}
	_, e = BuildResourcePlan(tpl.Manifest{Resources: &tpl.ResourcesSpec{CPU: "1", Memory: "2Gi", Requests: &tpl.ResourceAmounts{CPU: "2", Memory: "1Gi"}}})
	if e == nil {
		t.Fatal("oversized request allowed")
	}
}
func TestResourcePlanMallBudget(t *testing.T) {
	m, ok := tpl.Load("../../../../templates").Get("mall")
	if !ok {
		t.Fatal("mall missing")
	}
	p, e := BuildResourcePlan(m)
	if e != nil || p.Requests.CPUMilli != 625 || p.Requests.MemoryBytes != 2976*1024*1024 || p.Limits.CPUMilli != 4000 || p.StorageBytes != 5536*1024*1024 || len(p.Components) != 7 {
		t.Fatal(p, e)
	}
}
