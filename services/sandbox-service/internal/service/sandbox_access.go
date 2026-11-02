package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const projectAccessBinding = "sandbox-project-access"
const workspaceClosedAnnotation = "quenching.io/workspace-closed"

// ConfigureTerminalAccess selects the unbound permission template installed by
// Helm. Actual exec access is granted by a RoleBinding in each live sandbox.
func (c *K8sClient) ConfigureTerminalAccess(platformNamespace, clusterRole string) {
	c.terminalNamespace = platformNamespace
	c.terminalClusterRole = clusterRole
}

func validSandboxNamespace(namespace string) bool {
	id := strings.TrimPrefix(namespace, "sandbox-")
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && namespace == "sandbox-"+strconv.FormatInt(n, 10)
}

func (c *K8sClient) ensureSandboxNamespace(ctx context.Context, sid, uid string) (*core.Namespace, error) {
	want := BuildNamespace(sid, uid)
	if !validSandboxNamespace(want.Name) {
		return nil, fmt.Errorf("invalid sandbox namespace %q", want.Name)
	}
	ns, err := c.clientset.CoreV1().Namespaces().Create(ctx, want, meta.CreateOptions{})
	if isAlreadyExists(err) {
		ns, err = c.clientset.CoreV1().Namespaces().Get(ctx, want.Name, meta.GetOptions{})
	}
	if err != nil {
		return nil, err
	}
	if ns.Labels[LabelRole] != "sandbox" || ns.Labels[LabelSession] != sid || ns.Labels[LabelUser] != uid || ns.DeletionTimestamp != nil {
		return nil, fmt.Errorf("sandbox namespace ownership or lifecycle mismatch: %s", want.Name)
	}
	return ns, nil
}

func (c *K8sClient) projectAccess(namespace string, closed bool) *rbac.RoleBinding {
	platformNS, role := c.terminalNamespace, c.terminalClusterRole
	if platformNS == "" {
		platformNS = PlatformNamespace
	}
	if role == "" {
		role = "quenching-quenching-sandbox-terminal"
	}
	b := &rbac.RoleBinding{
		ObjectMeta: meta.ObjectMeta{Name: projectAccessBinding, Namespace: namespace},
		RoleRef:    rbac.RoleRef{APIGroup: rbac.GroupName, Kind: "ClusterRole", Name: role},
	}
	if closed {
		b.Annotations = map[string]string{workspaceClosedAnnotation: "true"}
	} else {
		b.Subjects = []rbac.Subject{{Kind: "ServiceAccount", Name: "sandbox-service", Namespace: platformNS}}
	}
	return b
}

// Keep a closed, subjectless binding until namespace deletion. This tombstone
// prevents a stale allocation/reconcile snapshot from reopening the workspace.
func (c *K8sClient) setSandboxAccess(ctx context.Context, namespace string, closed bool) error {
	want := c.projectAccess(namespace, closed)
	bindings := c.clientset.RbacV1().RoleBindings(namespace)
	return retry.OnError(retry.DefaultBackoff, func(err error) bool { return apierrors.IsConflict(err) || isAlreadyExists(err) }, func() error {
		b, err := bindings.Get(ctx, want.Name, meta.GetOptions{})
		if isNotFound(err) {
			_, err = bindings.Create(ctx, want, meta.CreateOptions{})
			return err
		}
		if err != nil {
			return err
		}
		if !closed && b.Annotations[workspaceClosedAnnotation] == "true" {
			return fmt.Errorf("training workspace is closed: %s", namespace)
		}
		if b.RoleRef != want.RoleRef {
			return fmt.Errorf("unexpected project access role in %s", namespace)
		}
		b.Subjects = want.Subjects
		if closed {
			if b.Annotations == nil {
				b.Annotations = map[string]string{}
			}
			b.Annotations[workspaceClosedAnnotation] = "true"
		}
		_, err = bindings.Update(ctx, b, meta.UpdateOptions{})
		return err
	})
}

// CloseSandboxAccess revokes only this training's exec authorization. It does
// not delete project data; evidence collection may finish before namespace GC.
func (c *K8sClient) CloseSandboxAccess(ctx context.Context, namespace string) error {
	if !validSandboxNamespace(namespace) {
		return fmt.Errorf("refuse project access outside a sandbox: %q", namespace)
	}
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, namespace, meta.GetOptions{})
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if ns.Labels[LabelRole] != "sandbox" || "sandbox-"+ns.Labels[LabelSession] != namespace {
		return fmt.Errorf("unmanaged sandbox namespace: %s", namespace)
	}
	return c.setSandboxAccess(ctx, namespace, true)
}
