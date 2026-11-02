package service

import (
	"context"
	"fmt"
	tpl "github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	app "k8s.io/api/apps/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("QUENCHING_TEST_HELM_INSTALL"); mode != "" {
		if mode == "wait" {
			time.Sleep(time.Minute)
		}
		fmt.Fprintln(os.Stderr, "workload pending")
		os.Exit(1)
	}
	if mode := os.Getenv("QUENCHING_TEST_HELM_STATUS"); mode != "" {
		if mode == "missing" {
			fmt.Fprintln(os.Stderr, "Error: release: not found")
		} else {
			fmt.Fprintln(os.Stderr, "Error: Kubernetes cluster unreachable")
		}
		os.Exit(1)
	}
	os.Exit(m.Run())
}
func TestHelmStatusUncertaintyIsNotAbsence(t *testing.T) {
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	h := &HelmCLI{BinPath: bin}
	t.Setenv("QUENCHING_TEST_HELM_STATUS", "offline")
	if exists, e := h.Exists(context.Background(), "sandbox-1", "mall"); e == nil || exists {
		t.Fatal("uncertain status reported absent", e)
	}
	t.Setenv("QUENCHING_TEST_HELM_STATUS", "missing")
	if exists, e := h.Exists(context.Background(), "sandbox-1", "mall"); e != nil || exists {
		t.Fatal(exists, e)
	}
}

type recoveryStore struct {
	Store
	sb             model.Sandbox
	ready, created int
}

func (s *recoveryStore) GetBySessionID(context.Context, int64) (*model.Sandbox, error) {
	v := s.sb
	return &v, nil
}
func (s *recoveryStore) MarkStatus(_ context.Context, _ int64, status string) error {
	s.sb.Status = status
	return nil
}
func (s *recoveryStore) MarkAllocationReady(context.Context, int64) error {
	s.ready++
	s.sb.Status = model.StatusReady
	return nil
}
func (s *recoveryStore) CreateQueued(context.Context, *model.Sandbox) error { s.created++; return nil }

type recoveryDeployer struct {
	installs, uninstalls int
	statusErr            error
}

func (d *recoveryDeployer) Exists(context.Context, string, string) (bool, error) {
	return true, d.statusErr
}
func (d *recoveryDeployer) Install(context.Context, ChartInfo) error { d.installs++; return nil }
func (d *recoveryDeployer) Uninstall(context.Context, string, string) error {
	d.uninstalls++
	return nil
}
func (d *recoveryDeployer) InspectRelease(context.Context, string, string) (HelmRelease, error) {
	return HelmRelease{Status: "deployed", Manifest: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: frontend\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: portal\n"}, nil
}
func TestAllocationRecoveryRejectsPartialRelease(t *testing.T) {
	reg := tpl.Load("../../../../templates")
	m, _ := reg.Get("mall")
	p, _ := BuildResourcePlan(m)
	store := &recoveryStore{sb: model.Sandbox{SessionID: 1, UserID: 2, Namespace: "sandbox-1", Status: model.StatusDeploying, TemplateSlug: "mall", TemplateDigest: m.ArtifactDigest, ResourcePlan: p, CreatedAt: time.Now()}}
	one := int32(1)
	dep := &app.Deployment{ObjectMeta: meta.ObjectMeta{Name: "frontend", Namespace: "sandbox-1"}, Spec: app.DeploymentSpec{Replicas: &one}, Status: app.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1, UpdatedReplicas: 1}}
	cs := fake.NewSimpleClientset(dep)
	d := &recoveryDeployer{}
	w := NewReadyWaiter(cs)
	w.Interval = time.Millisecond
	w.Timeout = 10 * time.Millisecond
	svc := NewSandboxService(NewReconciler(NewK8sClient(cs), store), nil, nil, store, NewProvisionService(d, w), reg)
	if e := svc.ConvergeAllocation(context.Background(), store.sb); e == nil || store.ready != 0 || store.sb.Status != model.StatusDeploying || d.installs != 0 || d.uninstalls != 0 {
		t.Fatal("partial recovery changed environment", e, store.ready, store.sb.Status, d)
	}
	portal := dep.DeepCopy()
	portal.Name = "portal"
	if _, e := cs.AppsV1().Deployments("sandbox-1").Create(context.Background(), portal, meta.CreateOptions{}); e != nil {
		t.Fatal(e)
	}
	if e := svc.ConvergeAllocation(context.Background(), store.sb); e != nil || store.ready != 1 || d.installs != 0 || d.uninstalls != 0 {
		t.Fatal("complete recovery failed", e, store.ready, d)
	}
	store.sb.Status = model.StatusDeploying
	d.statusErr = fmt.Errorf("offline")
	if e := svc.ConvergeAllocation(context.Background(), store.sb); e == nil || d.installs != 0 || d.uninstalls != 0 || store.sb.Status != model.StatusDeploying {
		t.Fatal("uncertain recovery mutated release", e, d)
	}
}
func TestClosedSessionCannotReenqueueAtCreateBoundary(t *testing.T) {
	reg := tpl.Load("../../../../templates")
	m, _ := reg.Get("mall")
	store := &recoveryStore{sb: model.Sandbox{SessionID: 1, UserID: 2, ProjectID: 0, TemplateSlug: "mall", TemplateDigest: m.ArtifactDigest, Status: model.StatusReleased}}
	svc := NewSandboxService(nil, nil, nil, store, nil, reg)
	if _, e := svc.enqueue(context.Background(), 2, 0, 1, m); e == nil || store.created != 0 {
		t.Fatal("closed session requeued", e, store.created)
	}
}
