package service

import (
	"context"
	tpl "github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"strconv"
	"testing"
)

func TestPoolQueueQuotaReleaseAndNewNode(t *testing.T) {
	ctx := context.Background()
	m, ok := tpl.Load("../../../../templates").Get("mall")
	if !ok {
		t.Fatal("mall missing")
	}
	plan, err := BuildResourcePlan(m)
	if err != nil {
		t.Fatal(err)
	}
	n := poolNode("first")
	n.Status.Allocatable[core.ResourceCPU] = resource.MustParse("1")
	platform := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "platform", Namespace: "platform"}, Spec: core.PodSpec{NodeName: n.Name, Containers: []core.Container{{Name: "app", Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("600m"), core.ResourceMemory: resource.MustParse("1Gi")}}}}}}
	client := fake.NewSimpleClientset(n, platform)
	pool := ResourcePool{Client: client, PlatformNamespace: "platform"}
	kc := NewK8sClient(client)
	store := &allocationFake{items: []model.Sandbox{{SessionID: 1, Namespace: "sandbox-1", Status: model.StatusQueued, ResourcePlan: plan}, {SessionID: 2, Namespace: "sandbox-2", Status: model.StatusQueued, ResourcePlan: plan}}}
	q := ResourceQueue{Store: store, Snapshot: pool.Snapshot, Deploy: func(ctx context.Context, sb model.Sandbox) error {
		store.starts++
		_, err := kc.EnsureSandboxResourcesWithPlan(ctx, strconv.FormatInt(sb.SessionID, 10), "8", sb.ResourcePlan)
		return err
	}}
	if err = q.Tick(ctx); err != nil || store.starts != 0 {
		t.Fatal("platform request not accounted", err, store.starts)
	}
	if _, err = client.CoreV1().Nodes().Create(ctx, poolNode("second"), meta.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	// Give only enough CPU for one training admission; reservations must block the second.
	second, _ := client.CoreV1().Nodes().Get(ctx, "second", meta.GetOptions{})
	second.Status.Allocatable[core.ResourceCPU] = resource.MustParse("550m")
	_, _ = client.CoreV1().Nodes().Update(ctx, second, meta.UpdateOptions{})
	if err = q.Tick(ctx); err != nil || store.starts != 1 || store.items[1].ResourcesAdmitted {
		t.Fatal("queue reservation", err, store.starts)
	}
	quota, err := client.CoreV1().ResourceQuotas("sandbox-1").Get(ctx, "sandbox-quota", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cpu := quota.Spec.Hard[core.ResourceRequestsCPU]
	storage := quota.Spec.Hard[core.ResourceRequestsStorage]
	if cpu.MilliValue() != plan.Requests.CPUMilli || storage.Value() != plan.StorageBytes {
		t.Fatal("quota differs from admission plan", quota)
	}
	store.items[0].Status = model.StatusReleased
	store.items[0].ResourcesAdmitted = false
	if err = q.Tick(ctx); err != nil || store.starts != 2 || !store.items[1].ResourcesAdmitted {
		t.Fatal("release did not free reservation", err, store.starts)
	}
}
