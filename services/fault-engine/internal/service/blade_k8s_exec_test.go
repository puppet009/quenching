package service

import (
	"context"
	"github.com/quenching/fault-engine/internal/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestMissingAgentNeverFallsBack(t *testing.T) {
	e := NewBladeK8sExecForClient(fake.NewSimpleClientset(), BladeCloudExecConfig{})
	if _, err := e.ResolveScope(context.Background(), model.InjectSpec{Scope: "pod", TargetNamespace: "sandbox-1", TargetPod: "portal"}); err == nil {
		t.Fatal("missing target/agent accepted")
	}
}

func TestTargetPodDriftRejected(t *testing.T) {
	target := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "portal", Namespace: "sandbox-7", UID: types.UID("new")}, Spec: corev1.PodSpec{NodeName: "node-new"}}
	agent := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "chaos", Labels: map[string]string{"app": "chaosblade-agent"}}, Spec: corev1.PodSpec{NodeName: "node-new"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	e := NewBladeK8sExecForClient(fake.NewSimpleClientset(target, agent), BladeCloudExecConfig{})
	if _, err := e.ResolveScope(context.Background(), model.InjectSpec{Scope: "pod", TargetNamespace: "sandbox-7", TargetPod: "portal", ExecNode: "node-old"}); err == nil {
		t.Fatal("executed stale Pod binding on another node")
	}
}

func TestRunningTemplateRevisionMustMatchAcceptance(t *testing.T) {
	target := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "portal", Namespace: "sandbox-7", Annotations: map[string]string{"quenching.io/template-digest": "old"}}, Spec: corev1.PodSpec{NodeName: "node"}}
	agent := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "chaosblade", Labels: map[string]string{"app": "chaosblade-agent"}}, Spec: corev1.PodSpec{NodeName: "node"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	e := NewBladeK8sExecForClient(fake.NewSimpleClientset(target, agent), BladeCloudExecConfig{AgentNamespace: "chaosblade"})
	if _, err := e.ResolveScope(context.Background(), model.InjectSpec{Scope: "pod", TargetNamespace: "sandbox-7", TargetPod: "portal", TemplateDigest: "accepted-new"}); err == nil {
		t.Fatal("old running template accepted")
	}
}
