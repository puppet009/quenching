package service

import (
	"context"
	"errors"
	"github.com/quenching/sandbox-service/internal/model"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type brainRuntimePurger interface {
	PurgeBrainRuntime(context.Context, int64) (bool, error)
}

func (r *Reconciler) CleanupBrainSession(ctx context.Context, sb *model.Sandbox) (string, error) {
	if r.kc == nil || !r.kc.RealCluster() {
		return "pending", errors.New("real Kubernetes connection required for cleanup verification")
	}
	if err := r.ReleaseSandbox(ctx, sb); err != nil {
		return "pending", err
	}
	_, err := r.kc.Clientset().CoreV1().Namespaces().Get(ctx, sb.Namespace, metav1.GetOptions{})
	if err == nil {
		return "pending", nil
	}
	if !apierrors.IsNotFound(err) {
		return "pending", err
	}
	purger, ok := r.db.(brainRuntimePurger)
	if !ok {
		return "pending", errors.New("runtime purge store unavailable")
	}
	done, err := purger.PurgeBrainRuntime(ctx, sb.SessionID)
	if err != nil {
		return "pending", err
	}
	if !done {
		return "pending", nil
	}
	return "succeeded", nil
}
