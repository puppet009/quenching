package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSandboxCreationGrantsOnlyNamespaceExecBinding(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	kc := NewK8sClient(cs)
	for i := 0; i < 2; i++ {
		if _, err := kc.EnsureSandboxResources(ctx, "71", "9"); err != nil {
			t.Fatal(err)
		}
	}
	bindings, err := cs.RbacV1().RoleBindings("sandbox-71").List(ctx, meta.ListOptions{})
	if err != nil || len(bindings.Items) != 1 {
		t.Fatalf("missing per-training authorization: %v %+v", err, bindings.Items)
	}
	b := bindings.Items[0]
	if b.Name != "sandbox-project-access" || b.RoleRef.Kind != "ClusterRole" || b.RoleRef.Name != "quenching-quenching-sandbox-terminal" || len(b.Subjects) != 1 || b.Subjects[0].Namespace != "platform" || b.Subjects[0].Name != "sandbox-service" {
		t.Fatalf("unexpected authorization: %+v", b)
	}
	all, _ := cs.RbacV1().RoleBindings("").List(ctx, meta.ListOptions{})
	global, _ := cs.RbacV1().ClusterRoleBindings().List(ctx, meta.ListOptions{})
	if len(all.Items) != 1 || len(global.Items) != 0 {
		t.Fatal("authorization escaped the training namespace")
	}
}

func TestSandboxAccessRejectsNamespaceWithDifferentOwner(t *testing.T) {
	ctx := context.Background()
	ns := BuildNamespace("71", "someone-else")
	cs := fake.NewSimpleClientset(ns)
	if _, err := NewK8sClient(cs).EnsureSandboxResources(ctx, "71", "9"); err == nil {
		t.Fatal("adopted another owner's namespace")
	}
	bindings, _ := cs.RbacV1().RoleBindings(ns.Name).List(ctx, meta.ListOptions{})
	if len(bindings.Items) != 0 {
		t.Fatal("granted access to another owner's sandbox")
	}
}

type accessLifecycleStore struct {
	Store
	items []model.Sandbox
}

type closedAccessGuardStore struct {
	accessLifecycleStore
	checked bool
}

func (s *closedAccessGuardStore) WithWorkspaceAccess(context.Context, int64, func(context.Context) error) error {
	s.checked = true
	return errors.New("training already completed before namespace creation")
}

func TestStaleAllocationCannotAuthorizeAfterCompletionWithoutNamespace(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	store := &closedAccessGuardStore{accessLifecycleStore: accessLifecycleStore{items: []model.Sandbox{{SessionID: 71, UserID: 9, Namespace: "sandbox-71", Status: model.StatusDeploying}}}}
	err := NewReconciler(NewK8sClient(cs), store).ReconcileOnce(ctx)
	if err == nil || !store.checked {
		t.Fatal("stale snapshot bypassed authoritative workspace gate")
	}
	ns, _ := cs.CoreV1().Namespaces().List(ctx, meta.ListOptions{})
	if len(ns.Items) != 0 {
		t.Fatal("created sandbox after completion acknowledgement")
	}
}

func (s *accessLifecycleStore) ListActive(context.Context) ([]model.Sandbox, error) {
	return s.items, nil
}

func TestReconcilerRevokesCompletedTrainingWithoutRegranting(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	kc := NewK8sClient(cs)
	store := &accessLifecycleStore{items: []model.Sandbox{
		{SessionID: 71, UserID: 9, Namespace: "sandbox-71", Status: model.StatusReady},
		{SessionID: 72, UserID: 10, Namespace: "sandbox-72", Status: model.StatusReady},
	}}
	r := NewReconciler(kc, store)
	if err := r.ReconcileOnce(ctx); err != nil {
		t.Fatal(err)
	}
	store.items[0].CompletedAt = time.Now()
	if err := r.ReconcileOnce(ctx); err != nil {
		t.Fatal(err)
	}
	bindings, _ := cs.RbacV1().RoleBindings("sandbox-71").List(ctx, meta.ListOptions{})
	for _, b := range bindings.Items {
		if len(b.Subjects) != 0 {
			t.Fatal("completed training still has terminal access")
		}
	}
	// A stale allocation/reconcile snapshot must not undo revocation.
	if _, err := kc.EnsureSandboxResources(ctx, "71", "9"); err == nil {
		t.Fatal("reopened a closed training workspace")
	}
	b, err := cs.RbacV1().RoleBindings("sandbox-72").Get(ctx, "sandbox-project-access", meta.GetOptions{})
	if err != nil || len(b.Subjects) != 1 {
		t.Fatal("revocation affected another training", err)
	}
}

func TestSandboxDeletionRemovesProjectAuthorization(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	kc := NewK8sClient(cs)
	if _, err := kc.EnsureSandboxResources(ctx, "71", "9"); err != nil {
		t.Fatal(err)
	}
	if err := kc.DeleteSandbox(ctx, "sandbox-71"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CoreV1().Namespaces().Get(ctx, "sandbox-71", meta.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("sandbox not deleted", err)
	}
	// Fake Kubernetes does not garbage collect namespace children. The binding must
	// already have no subject before the namespace deletion request.
	bindings, _ := cs.RbacV1().RoleBindings("sandbox-71").List(ctx, meta.ListOptions{})
	for _, b := range bindings.Items {
		if len(b.Subjects) != 0 {
			t.Fatal("deleted sandbox retained authorization")
		}
	}
}

func TestSandboxCreationRefusesTerminatingNamespace(t *testing.T) {
	ns := BuildNamespace("71", "9")
	now := meta.Now()
	ns.DeletionTimestamp = &now
	ns.Status.Phase = core.NamespaceTerminating
	if _, err := NewK8sClient(fake.NewSimpleClientset(ns)).EnsureSandboxResources(context.Background(), "71", "9"); err == nil {
		t.Fatal("reopened a terminating namespace")
	}
}
