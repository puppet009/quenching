package service

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"time"
)

type releaseObject struct {
	Kind     string
	Metadata struct{ Name, Namespace string }
}

func (w *ReadyWaiter) WaitForRelease(ctx context.Context, namespace string, release HelmRelease) error {
	if release.Status != "deployed" {
		return fmt.Errorf("release is %s; waiting for recovery", release.Status)
	}
	var inventory []releaseObject
	dec := yaml.NewDecoder(strings.NewReader(release.Manifest))
	controllers := 0
	for {
		var obj releaseObject
		e := dec.Decode(&obj)
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if obj.Kind == "" {
			continue
		}
		if obj.Metadata.Name == "" || obj.Metadata.Namespace != "" && obj.Metadata.Namespace != namespace {
			return fmt.Errorf("invalid release resource scope")
		}
		if obj.Kind == "Deployment" || obj.Kind == "StatefulSet" || obj.Kind == "Pod" || obj.Kind == "Job" {
			controllers++
		}
		inventory = append(inventory, obj)
	}
	if controllers == 0 {
		return fmt.Errorf("release has no verifiable workload inventory")
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	interval := w.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last error
	for {
		ready := true
		for _, obj := range inventory {
			ok, e := w.releaseObjectReady(ctx, namespace, obj)
			if e != nil || !ok {
				ready = false
				if e != nil {
					last = e
				} else {
					last = fmt.Errorf("%s/%s not ready", obj.Kind, obj.Metadata.Name)
				}
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("release inventory incomplete: %v: %w", last, ctx.Err())
		case <-ticker.C:
		}
	}
}
func (w *ReadyWaiter) releaseObjectReady(ctx context.Context, ns string, obj releaseObject) (bool, error) {
	name := obj.Metadata.Name
	opts := meta.GetOptions{}
	switch obj.Kind {
	case "Deployment":
		v, e := w.clientset.AppsV1().Deployments(ns).Get(ctx, name, opts)
		if e != nil {
			return false, e
		}
		r := int32(1)
		if v.Spec.Replicas != nil {
			r = *v.Spec.Replicas
		}
		return r > 0 && v.Status.ObservedGeneration >= v.Generation && v.Status.ReadyReplicas >= r && v.Status.UpdatedReplicas >= r && v.Status.AvailableReplicas >= r, nil
	case "StatefulSet":
		v, e := w.clientset.AppsV1().StatefulSets(ns).Get(ctx, name, opts)
		if e != nil {
			return false, e
		}
		r := int32(1)
		if v.Spec.Replicas != nil {
			r = *v.Spec.Replicas
		}
		return r > 0 && v.Status.ObservedGeneration >= v.Generation && v.Status.ReadyReplicas >= r && v.Status.UpdatedReplicas >= r, nil
	case "PersistentVolumeClaim":
		v, e := w.clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, name, opts)
		if e != nil {
			return false, e
		}
		return v.Status.Phase == core.ClaimBound, nil
	case "Pod":
		v, e := w.clientset.CoreV1().Pods(ns).Get(ctx, name, opts)
		if e != nil {
			return false, e
		}
		for _, c := range v.Status.Conditions {
			if c.Type == core.PodReady && c.Status == core.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	case "Job":
		v, e := w.clientset.BatchV1().Jobs(ns).Get(ctx, name, opts)
		if e != nil {
			return false, e
		}
		for _, c := range v.Status.Conditions {
			if c.Type == "Complete" && c.Status == core.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	case "Service":
		_, e := w.clientset.CoreV1().Services(ns).Get(ctx, name, opts)
		return e == nil, e
	case "Secret":
		_, e := w.clientset.CoreV1().Secrets(ns).Get(ctx, name, opts)
		return e == nil, e
	case "ConfigMap":
		_, e := w.clientset.CoreV1().ConfigMaps(ns).Get(ctx, name, opts)
		return e == nil, e
	case "ServiceAccount":
		_, e := w.clientset.CoreV1().ServiceAccounts(ns).Get(ctx, name, opts)
		return e == nil, e
	case "NetworkPolicy":
		_, e := w.clientset.NetworkingV1().NetworkPolicies(ns).Get(ctx, name, opts)
		return e == nil, e
	case "Ingress":
		_, e := w.clientset.NetworkingV1().Ingresses(ns).Get(ctx, name, opts)
		return e == nil, e
	case "Role":
		_, e := w.clientset.RbacV1().Roles(ns).Get(ctx, name, opts)
		return e == nil, e
	case "RoleBinding":
		_, e := w.clientset.RbacV1().RoleBindings(ns).Get(ctx, name, opts)
		return e == nil, e
	case "LimitRange":
		_, e := w.clientset.CoreV1().LimitRanges(ns).Get(ctx, name, opts)
		return e == nil, e
	case "ResourceQuota":
		_, e := w.clientset.CoreV1().ResourceQuotas(ns).Get(ctx, name, opts)
		return e == nil, e
	default:
		return false, fmt.Errorf("unsupported release inventory kind %s", obj.Kind)
	}
}
func (s *SandboxService) verifyAllocatedRelease(ctx context.Context, ns, release string) error {
	inspector, ok := s.provision.deployer.(HelmReleaseInspector)
	if !ok || s.provision.waiter == nil {
		return fmt.Errorf("release recovery verifier unavailable")
	}
	view, e := inspector.InspectRelease(ctx, ns, release)
	if e != nil {
		return e
	}
	return s.provision.waiter.WaitForRelease(ctx, ns, view)
}
