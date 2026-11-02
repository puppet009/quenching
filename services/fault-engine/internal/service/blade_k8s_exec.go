package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/quenching/fault-engine/internal/model"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type BladeK8sExec struct {
	client  kubernetes.Interface
	rest    *rest.Config
	cfg     BladeCloudExecConfig
	initErr error
}

func NewBladeK8sExec(cfg BladeCloudExecConfig) (*BladeK8sExec, error) {
	e := NewBladeK8sExecForClient(nil, cfg)
	var err error
	if cfg.Kubeconfig != "" {
		e.rest, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		e.rest, err = rest.InClusterConfig()
	}
	if err != nil {
		e.initErr = err
		return e, err
	}
	e.client, err = kubernetes.NewForConfig(e.rest)
	e.initErr = err
	return e, err
}
func NewBladeK8sExecForClient(client kubernetes.Interface, cfg BladeCloudExecConfig) *BladeK8sExec {
	return &BladeK8sExec{client: client, cfg: NewBladeCloudExec(cfg).cfg}
}
func (e *BladeK8sExec) Ready(ctx context.Context) error {
	if !e.cfg.Enabled {
		return errors.New("fault execution disabled")
	}
	if e.client == nil {
		return errors.New("Kubernetes fault execution unavailable")
	}
	permission, err := e.client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authv1.ResourceAttributes{Namespace: e.cfg.AgentNamespace, Verb: "create", Resource: "pods", Subresource: "exec"}}}, metav1.CreateOptions{})
	if err != nil || !permission.Status.Allowed {
		return errors.New("agent exec permission unavailable")
	}
	operator, err := e.client.AppsV1().Deployments(e.cfg.AgentNamespace).Get(ctx, e.cfg.OperatorName, metav1.GetOptions{})
	if err != nil || operator.Status.ReadyReplicas < 1 {
		return errors.New("ChaosBlade operator is unavailable or unreadable")
	}
	pods, err := e.client.CoreV1().Pods(e.cfg.AgentNamespace).List(ctx, metav1.ListOptions{LabelSelector: e.cfg.AgentPodSelector})
	if err != nil {
		return err
	}
	for _, p := range pods.Items {
		if bladeAgentReady(p) {
			return nil
		}
	}
	return errors.New("ChaosBlade agent unavailable")
}
func bladeAgentReady(p corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
func (e *BladeK8sExec) ResolveScope(ctx context.Context, spec model.InjectSpec) (model.InjectSpec, error) {
	if e.client == nil || spec.Scope != "pod" || !sandboxName.MatchString(spec.TargetNamespace) || !podName.MatchString(spec.TargetPod) {
		return spec, errors.New("specific sandbox Pod required")
	}
	pod, err := e.client.CoreV1().Pods(spec.TargetNamespace).Get(ctx, spec.TargetPod, metav1.GetOptions{})
	if err != nil {
		return spec, err
	}
	if spec.TemplateDigest != "" && pod.Annotations["quenching.io/template-digest"] != spec.TemplateDigest {
		return spec, errors.New("running template revision differs from accepted fault")
	}
	if pod.Spec.NodeName == "" {
		return spec, errors.New("target Pod has no node")
	}
	if spec.ExecNode != "" && spec.ExecNode != pod.Spec.NodeName {
		return spec, errors.New("target Pod node changed")
	}
	if spec.TargetPodUID != "" && spec.TargetPodUID != string(pod.UID) {
		return spec, errors.New("target Pod replaced")
	}
	spec.TargetPodUID = string(pod.UID)
	spec.ExecNode = pod.Spec.NodeName
	if _, err = e.agentForNode(ctx, spec.ExecNode); err != nil {
		return spec, err
	}
	return spec, nil
}
func (e *BladeK8sExec) agentForNode(ctx context.Context, node string) (string, error) {
	pods, err := e.client.CoreV1().Pods(e.cfg.AgentNamespace).List(ctx, metav1.ListOptions{LabelSelector: e.cfg.AgentPodSelector})
	if err != nil {
		return "", err
	}
	name := ""
	for _, p := range pods.Items {
		if p.Spec.NodeName == node && bladeAgentReady(p) {
			if name != "" {
				return "", errors.New("ambiguous agent on node")
			}
			name = p.Name
		}
	}
	if name == "" {
		return "", errors.New("target node has no ready agent")
	}
	return name, nil
}
func (e *BladeK8sExec) Run(ctx context.Context, spec model.InjectSpec, args []string) (*BladeExecResult, error) {
	if !e.cfg.Enabled || e.client == nil || e.rest == nil || spec.Scope != "pod" || !sandboxName.MatchString(spec.TargetNamespace) || len(args) == 0 {
		return nil, errors.New("sandbox fault execution unavailable")
	}
	var err error
	if args[0] == "create" || spec.ExecNode == "" {
		spec, err = e.ResolveScope(ctx, spec)
		if err != nil {
			return nil, err
		}
	}
	if args[0] != "create" && args[0] != "destroy" && args[0] != "status" {
		return nil, errors.New("unsupported blade action")
	}
	agent, err := e.agentForNode(ctx, spec.ExecNode)
	if err != nil {
		return nil, err
	}
	cmd := append([]string{e.cfg.BladePath}, args...)
	req := e.client.CoreV1().RESTClient().Post().Resource("pods").Namespace(e.cfg.AgentNamespace).Name(agent).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: e.cfg.Container, Command: cmd, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(e.rest, "POST", req.URL())
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	if err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return nil, fmt.Errorf("scoped blade execution failed: %w", err)
	}
	return &BladeExecResult{Raw: stdout.String()}, nil
}
