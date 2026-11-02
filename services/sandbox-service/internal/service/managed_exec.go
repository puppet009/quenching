package service

import (
	"context"
	"errors"
	"fmt"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/sandbox-service/internal/model"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

type K8sManagedExecutor struct{ Client *K8sClient }

func (x K8sManagedExecutor) Prepare(ctx context.Context, sid int64, t oe.Target) (oe.Target, error) {
	if x.Client == nil || x.Client.restCfg == nil {
		return t, fmt.Errorf("real Kubernetes required")
	}
	targets, err := x.Client.ListTargets(ctx, sid)
	if err != nil {
		return t, err
	}
	allowed := false
	for _, target := range targets {
		if target.Name == t.Pod && target.Ready {
			allowed = true
		}
	}
	if !allowed {
		return t, oe.ErrNotFound
	}
	pod, err := x.Client.clientset.CoreV1().Pods(model.NamespaceName(sid)).Get(ctx, t.Pod, metav1.GetOptions{})
	if err != nil {
		return t, err
	}
	t.PodUID = string(pod.UID)
	if t.Container == "" {
		t.Container = pod.Spec.Containers[0].Name
	}
	found := false
	for _, c := range pod.Spec.Containers {
		if c.Name == t.Container {
			found = true
		}
	}
	if !found {
		return t, oe.ErrNotFound
	}
	return t, nil
}

// Target namespace is fixed by the execution context; user input cannot select a namespace.
type managedSessionKey struct{}

func (x K8sManagedExecutor) Execute(ctx context.Context, t oe.Target, command string, out, errOut io.Writer) (*int, error) {
	sid, ok := ctx.Value(managedSessionKey{}).(int64)
	if !ok {
		return nil, fmt.Errorf("missing execution session")
	}
	ns := model.NamespaceName(sid)
	pod, err := x.Client.clientset.CoreV1().Pods(ns).Get(ctx, t.Pod, metav1.GetOptions{})
	if err != nil || string(pod.UID) != t.PodUID {
		return nil, fmt.Errorf("execution target replaced")
	}
	argv := []string{"/bin/sh", "-c", `cd -- "$1" && exec /bin/sh -c "$2"`, "quenching-command", t.Cwd, command}
	url := x.Client.clientset.CoreV1().RESTClient().Post().Namespace(ns).Resource("pods").Name(t.Pod).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: t.Container, Command: argv, Stdout: true, Stderr: true, TTY: false}, scheme.ParameterCodec).URL()
	exec, err := remotecommand.NewSPDYExecutor(x.Client.restCfg, "POST", url)
	if err != nil {
		return nil, err
	}
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: out, Stderr: errOut})
	if err == nil {
		zero := 0
		return &zero, nil
	}
	var code utilexec.ExitError
	if errors.As(err, &code) {
		n := code.ExitStatus()
		return &n, err
	}
	return nil, err
}
