package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHelmInstallPreservesDeadlineCause(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUENCHING_TEST_HELM_INSTALL", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err = (&HelmCLI{BinPath: bin}).Install(ctx, ChartInfo{Name: "mall", Namespace: "sandbox-5", ChartPath: "unused"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost deadline cause: %v", err)
	}
	if !strings.Contains(err.Error(), "sandbox-5") {
		t.Fatalf("lost namespace: %v", err)
	}
}

func TestHelmInstallOrdinaryFailureIsNotTimeout(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUENCHING_TEST_HELM_INSTALL", "fail")
	err = (&HelmCLI{BinPath: bin}).Install(context.Background(), ChartInfo{Name: "mall", Namespace: "sandbox-5", ChartPath: "unused"})
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "workload pending") {
		t.Fatalf("misclassified failure: %v", err)
	}
}

type failingProvisionDeployer struct{ client *fake.Clientset }

func (d failingProvisionDeployer) Exists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (d failingProvisionDeployer) Install(ctx context.Context, _ ChartInfo) error {
	<-ctx.Done()
	return ctx.Err()
}
func (d failingProvisionDeployer) Uninstall(ctx context.Context, ns, _ string) error {
	return d.client.CoreV1().Pods(ns).Delete(ctx, "portal", meta.DeleteOptions{})
}

func TestProvisionCapturesStartupEvidenceBeforeRollback(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&core.Pod{ObjectMeta: meta.ObjectMeta{Name: "portal", Namespace: "sandbox-5"}, Status: core.PodStatus{Phase: core.PodPending, InitContainerStatuses: []core.ContainerStatus{{Name: "wait-for-dependencies", State: core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "PodInitializing"}}}}, ContainerStatuses: []core.ContainerStatus{{Name: "portal", RestartCount: 3, State: core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}}},
		&core.Event{ObjectMeta: meta.ObjectMeta{Name: "failed-create", Namespace: "sandbox-5"}, Type: core.EventTypeWarning, Reason: "FailedCreate", Message: "exceeded quota"},
		&core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Name: "mysql-data", Namespace: "sandbox-5"}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimPending}},
		&core.Pod{ObjectMeta: meta.ObjectMeta{Name: "other-session-secret", Namespace: "sandbox-6"}},
	)
	svc := NewProvisionService(failingProvisionDeployer{cs}, NewReadyWaiter(cs))
	err := svc.Provision(context.Background(), ProvisionOpts{Namespace: "sandbox-5", ReleaseName: "mall", ChartPath: "unused", Timeout: time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost timeout: %v", err)
	}
	for _, part := range []string{"portal", "CrashLoopBackOff", "FailedCreate", "exceeded quota", "mysql-data"} {
		if !strings.Contains(fmt.Sprint(err), part) {
			t.Fatalf("missing startup evidence %s: %v", part, err)
		}
	}
	if strings.Contains(err.Error(), "other-session-secret") {
		t.Fatal("diagnostics crossed namespace")
	}
	if _, e := cs.CoreV1().Pods("sandbox-5").Get(context.Background(), "portal", meta.GetOptions{}); e == nil {
		t.Fatal("rollback did not remove failed workload")
	}
}
