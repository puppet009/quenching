package service

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestFaultReaderIsScopedAndOptIn(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	kc := NewK8sClient(cs)
	if _, e := kc.EnsureSandboxResources(ctx, "71", "1"); e != nil {
		t.Fatal(e)
	}
	roles, _ := cs.RbacV1().Roles("sandbox-71").List(ctx, metav1.ListOptions{})
	if len(roles.Items) != 0 {
		t.Fatal("created permissions while disabled")
	}
	kc.ConfigureFaultReader("platform", "fault-engine", true)
	if _, e := kc.EnsureSandboxResources(ctx, "71", "1"); e != nil {
		t.Fatal(e)
	}
	roles, _ = cs.RbacV1().Roles("sandbox-71").List(ctx, metav1.ListOptions{})
	if len(roles.Items) != 1 {
		t.Fatal("missing scoped role")
	}
	rule := roles.Items[0].Rules[0]
	if len(rule.Resources) != 1 || rule.Resources[0] != "pods" || len(rule.Verbs) != 2 {
		t.Fatal("excessive fault permissions")
	}
	binding, e := cs.RbacV1().RoleBindings("sandbox-71").Get(ctx, "fault-target-reader", metav1.GetOptions{})
	if e != nil || binding.Subjects[0].Namespace != "platform" {
		t.Fatal("wrong subject")
	}
}
