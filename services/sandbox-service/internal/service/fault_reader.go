package service

import (
	"context"
	"errors"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

func (c *K8sClient) ConfigureFaultReader(namespace, account string, enabled bool) {
	c.faultReaderNamespace = namespace
	c.faultReaderAccount = account
	c.faultReaderEnabled = enabled
}
func (c *K8sClient) ensureFaultReader(ctx context.Context, ns string) error {
	if !c.faultReaderEnabled {
		return nil
	}
	if !strings.HasPrefix(ns, "sandbox-") || c.faultReaderNamespace == "" || c.faultReaderAccount == "" {
		return errors.New("invalid fault reader configuration")
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "fault-target-reader", Namespace: ns}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}}}}
	if _, e := c.clientset.RbacV1().Roles(ns).Create(ctx, role, metav1.CreateOptions{}); e != nil {
		if !isAlreadyExists(e) {
			return e
		}
		existing, e := c.clientset.RbacV1().Roles(ns).Get(ctx, role.Name, metav1.GetOptions{})
		if e != nil {
			return e
		}
		role.ResourceVersion = existing.ResourceVersion
		if _, e = c.clientset.RbacV1().Roles(ns).Update(ctx, role, metav1.UpdateOptions{}); e != nil {
			return e
		}
	}
	binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name, Namespace: ns}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: c.faultReaderAccount, Namespace: c.faultReaderNamespace}}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: role.Name}}
	if _, e := c.clientset.RbacV1().RoleBindings(ns).Create(ctx, binding, metav1.CreateOptions{}); e != nil {
		if !isAlreadyExists(e) {
			return e
		}
		existing, e := c.clientset.RbacV1().RoleBindings(ns).Get(ctx, binding.Name, metav1.GetOptions{})
		if e != nil {
			return e
		}
		binding.ResourceVersion = existing.ResourceVersion
		_, e = c.clientset.RbacV1().RoleBindings(ns).Update(ctx, binding, metav1.UpdateOptions{})
		return e
	}
	return nil
}
