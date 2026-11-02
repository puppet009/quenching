package service

import (
	"sort"
	"strconv"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
)

// BuildAllocationUnits projects the final snapshot into platform, session, and
// infrastructure units. It does not calculate new reservations or change the
// scheduler's available capacity; NamespaceReserved records those deductions.
func BuildAllocationUnits(snapshot model.PoolSnapshot, platformNamespace string) []model.ResourceAllocationUnit {
	type aggregate struct {
		unit   model.ResourceAllocationUnit
		usage  model.ResourceAmounts
		oldest time.Time
	}
	platform := &aggregate{unit: model.ResourceAllocationUnit{ID: "platform:" + platformNamespace, Kind: "platform", Namespace: platformNamespace}}
	infrastructure := &aggregate{unit: model.ResourceAllocationUnit{ID: "infrastructure", Kind: "infrastructure"}}
	sandboxes := map[string]*aggregate{}
	for _, allocation := range snapshot.Allocations {
		namespace := allocation.Namespace
		if namespace == "" && allocation.SessionID > 0 {
			namespace = model.NamespaceName(allocation.SessionID)
		}
		id := "sandbox:" + namespace
		if allocation.SessionID > 0 {
			id = "sandbox:" + strconv.FormatInt(allocation.SessionID, 10)
		}
		plan := allocation.ResourcePlan
		unit := model.ResourceAllocationUnit{
			ID: id, Kind: "sandbox", Namespace: namespace,
			SessionID: allocation.SessionID, UserID: allocation.UserID, ProjectID: allocation.ProjectID,
			TemplateSlug: allocation.TemplateSlug, Status: allocation.Status,
			ResourcesAdmitted: allocation.ResourcesAdmitted, AllocationReason: allocation.AllocationReason,
			ResourcePlan: &plan,
		}
		if !allocation.CreatedAt.IsZero() {
			createdAt := allocation.CreatedAt
			unit.CreatedAt = &createdAt
		}
		sandboxes[namespace] = &aggregate{unit: unit}
	}
	pool := ResourcePool{PlatformNamespace: platformNamespace}
	forNamespace := func(namespace string) *aggregate {
		if namespace == platformNamespace {
			return platform
		}
		if unit, ok := sandboxes[namespace]; ok {
			return unit
		}
		if pool.group(namespace) == "training" {
			unit := &aggregate{unit: model.ResourceAllocationUnit{ID: "sandbox:" + namespace, Kind: "sandbox", Namespace: namespace}}
			sandboxes[namespace] = unit
			return unit
		}
		return infrastructure
	}
	for _, pod := range snapshot.Pods {
		a := forNamespace(pod.Namespace)
		a.unit.Requested = a.unit.Requested.Add(pod.Requests)
		a.unit.Limits = a.unit.Limits.Add(pod.Limits)
		a.unit.PodCount++
		if usage, fresh := FreshUsage(pod.Usage, snapshot.ObservedAt); fresh {
			a.usage = a.usage.Add(usage)
			a.unit.UsageSampledPods++
			at, _ := time.Parse(time.RFC3339Nano, pod.Usage.Timestamp)
			if a.oldest.IsZero() || at.Before(a.oldest) {
				a.oldest = at
			}
		}
	}
	for _, pvc := range snapshot.PVCs {
		a := forNamespace(pvc.Namespace)
		a.unit.StorageBytes += pvc.Bytes
	}
	// A workload can exist before its first Pod, or survive its session record.
	for _, workload := range snapshot.Workloads {
		forNamespace(workload.Namespace)
	}
	for namespace, reserved := range snapshot.NamespaceReserved {
		a := forNamespace(namespace)
		a.unit.Reserved = a.unit.Reserved.Add(reserved)
	}
	ordered := make([]*aggregate, 0, len(sandboxes))
	for _, a := range sandboxes {
		ordered = append(ordered, a)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i].unit, ordered[j].unit
		if left.SessionID > 0 && right.SessionID > 0 {
			return left.SessionID < right.SessionID
		}
		if (left.SessionID > 0) != (right.SessionID > 0) {
			return left.SessionID > 0
		}
		return left.Namespace < right.Namespace
	})
	ordered = append([]*aggregate{platform}, ordered...)
	ordered = append(ordered, infrastructure)
	units := make([]model.ResourceAllocationUnit, 0, len(ordered))
	for _, a := range ordered {
		// A partial sum would look like a complete total. Empty units also have no
		// measured usage, rather than an invented zero measurement.
		if a.unit.PodCount > 0 && a.unit.UsageSampledPods == a.unit.PodCount {
			a.unit.Usage = &model.ResourceUsage{CPU: a.usage, Timestamp: a.oldest.Format(time.RFC3339Nano)}
		}
		units = append(units, a.unit)
	}
	return units
}
