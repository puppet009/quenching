package service

import (
	"context"
	"github.com/quenching/sandbox-service/internal/model"
	app "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestNodeVersionAndRemovalGuard(t *testing.T) {
	n := poolNode("one")
	n.ResourceVersion = "1"
	n.UID = "uid"
	c := ResourceControls{Client: fake.NewSimpleClientset(n), PlatformNamespace: "platform"}
	ctx := context.Background()
	if e := c.SetNodeScheduling(ctx, "one", "stale", "uid", false); e == nil {
		t.Fatal("stale update allowed")
	}
	if e := c.SetNodeScheduling(ctx, "one", "1", "uid", false); e != nil {
		t.Fatal(e)
	}
	if e := c.RemoveNode(ctx, "one", "1", "uid"); e == nil {
		t.Fatal("ready node deleted")
	}
}
func TestWorkloadResourceValidation(t *testing.T) {
	d := &app.Deployment{ObjectMeta: meta.ObjectMeta{Name: "api", Namespace: "platform", ResourceVersion: "1"}, Spec: app.DeploymentSpec{Template: core.PodTemplateSpec{Spec: core.PodSpec{Containers: []core.Container{{Name: "app"}}}}}}
	client := fake.NewSimpleClientset(d)
	c := ResourceControls{Client: client, PlatformNamespace: "platform"}
	req := model.ResourceAmounts{CPUMilli: 100, MemoryBytes: 1000}
	lim := model.ResourceAmounts{CPUMilli: 500, MemoryBytes: 2000}
	ctx := context.Background()
	if e := c.UpdatePlatformWorkload(ctx, "sandbox-1", "Deployment", "api", "app", "1", req, lim); e == nil {
		t.Fatal("training workload mutated")
	}
	if e := c.UpdatePlatformWorkload(ctx, "platform", "Deployment", "api", "app", "1", lim, req); e == nil {
		t.Fatal("invalid limits")
	}
	if e := c.UpdatePlatformWorkload(ctx, "platform", "Deployment", "api", "app", "1", req, lim); e != nil {
		t.Fatal(e)
	}
	v, _ := client.AppsV1().Deployments("platform").Get(ctx, "api", meta.GetOptions{})
	if v.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue() != 100 {
		t.Fatal(v)
	}
}

func TestNodeRemovalBlocksMatchingLocalAffinity(t *testing.T) {
	for _, op := range []core.NodeSelectorOperator{core.NodeSelectorOpExists, core.NodeSelectorOpNotIn, core.NodeSelectorOpDoesNotExist} {
		t.Run(string(op), func(t *testing.T) {
			n := poolNode("one")
			n.ResourceVersion = "1"
			n.UID = "uid"
			n.Spec.Unschedulable = true
			n.Status.Conditions = nil
			n.Labels = map[string]string{"storage-node": "present"}
			key := "storage-node"
			values := []string(nil)
			if op == core.NodeSelectorOpNotIn {
				values = []string{"other"}
			}
			if op == core.NodeSelectorOpDoesNotExist {
				key = "missing-label"
			}
			pv := &core.PersistentVolume{ObjectMeta: meta.ObjectMeta{Name: "data"}, Spec: core.PersistentVolumeSpec{PersistentVolumeSource: core.PersistentVolumeSource{Local: &core.LocalVolumeSource{Path: "/data"}}, NodeAffinity: &core.VolumeNodeAffinity{Required: &core.NodeSelector{NodeSelectorTerms: []core.NodeSelectorTerm{{MatchExpressions: []core.NodeSelectorRequirement{{Key: key, Operator: op, Values: values}}}}}}}}
			cs := fake.NewSimpleClientset(n, pv)
			c := ResourceControls{Client: cs}
			if e := c.RemoveNode(context.Background(), "one", "1", "uid"); e == nil {
				t.Fatal("local PV node deleted")
			}
			if _, e := cs.CoreV1().Nodes().Get(context.Background(), "one", meta.GetOptions{}); e != nil {
				t.Fatal(e)
			}
		})
	}
}
