package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StartupDiagnostics is a bounded, namespace-scoped status snapshot. It never
// reads Secrets, container environment variables or application log contents.
func (w *ReadyWaiter) StartupDiagnostics(ctx context.Context, namespace string) string {
	var parts []string
	if pods, err := w.clientset.CoreV1().Pods(namespace).List(ctx, meta.ListOptions{Limit: 20}); err != nil {
		parts = append(parts, "pods: "+shortDiagnostic(err.Error()))
	} else {
		for i, pod := range pods.Items {
			if i >= 12 {
				break
			}
			parts = append(parts, fmt.Sprintf("pod %s phase=%s", pod.Name, pod.Status.Phase))
			for _, condition := range pod.Status.Conditions {
				if condition.Type == core.PodScheduled && condition.Status != core.ConditionTrue {
					parts = append(parts, shortDiagnostic(condition.Reason+": "+condition.Message))
				}
			}
			statuses := append(append([]core.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
			for _, container := range statuses {
				if state := container.State.Waiting; state != nil {
					parts = append(parts, shortDiagnostic(container.Name+": "+state.Reason+" "+state.Message))
				}
				if state := container.State.Terminated; state != nil {
					parts = append(parts, fmt.Sprintf("%s: %s exit=%d", container.Name, state.Reason, state.ExitCode))
				}
				if state := container.LastTerminationState.Terminated; state != nil {
					parts = append(parts, fmt.Sprintf("%s previous=%s exit=%d restarts=%d", container.Name, state.Reason, state.ExitCode, container.RestartCount))
				}
			}
		}
	}
	if claims, err := w.clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, meta.ListOptions{Limit: 20}); err != nil {
		parts = append(parts, "pvc: "+shortDiagnostic(err.Error()))
	} else {
		for i, claim := range claims.Items {
			if i >= 12 {
				break
			}
			parts = append(parts, fmt.Sprintf("pvc %s phase=%s", claim.Name, claim.Status.Phase))
		}
	}
	if events, err := w.clientset.CoreV1().Events(namespace).List(ctx, meta.ListOptions{FieldSelector: "type=Warning"}); err != nil {
		parts = append(parts, "events: "+shortDiagnostic(err.Error()))
	} else {
		sort.SliceStable(events.Items, func(i, j int) bool {
			return events.Items[i].CreationTimestamp.After(events.Items[j].CreationTimestamp.Time)
		})
		for i, event := range events.Items {
			if i >= 8 {
				break
			}
			parts = append(parts, shortDiagnostic(event.Reason+": "+event.Message))
		}
	}
	if len(parts) == 0 {
		return "namespace 中没有可见的 Pod/PVC/Warning 事件"
	}
	result := strings.Join(parts, "; ")
	runes := []rune(result)
	if len(runes) > 4000 {
		result = string(runes[:4000]) + "…"
	}
	return result
}

func shortDiagnostic(value string) string {
	runes := []rune(strings.ReplaceAll(value, "\n", " "))
	if len(runes) > 350 {
		return string(runes[:350]) + "…"
	}
	return string(runes)
}
