package service

import (
	"testing"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
)

func TestAllocationUnitsKeepSessionsQueueAndOrphanResources(t *testing.T) {
	at := time.Now().UTC()
	snapshot := model.PoolSnapshot{
		ObservedAt: at,
		Allocations: []model.Sandbox{
			{ID: 20, SessionID: 2, UserID: 7, ProjectID: 8, Namespace: "sandbox-2", Status: model.StatusDeploying, ResourcesAdmitted: true, ResourcePlan: model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 500}}},
			{ID: 10, SessionID: 1, UserID: 7, ProjectID: 8, Namespace: "sandbox-1", Status: model.StatusQueued, TemplateSlug: "mall", AllocationReason: "waiting", CreatedAt: at, ResourcePlan: model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 900}, StorageBytes: 1000}},
			{ID: 30, SessionID: 3, Namespace: "sandbox-3", Status: model.StatusReady, ResourcesAdmitted: true, ResourcePlan: model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 500}}},
		},
		Pods: []model.ResourcePod{
			{Namespace: "control", Name: "old", Requests: model.ResourceAmounts{CPUMilli: 100}, Limits: model.ResourceAmounts{CPUMilli: 200}},
			{Namespace: "control", Name: "replacement", Requests: model.ResourceAmounts{CPUMilli: 150}, Limits: model.ResourceAmounts{CPUMilli: 300}},
			{Namespace: "sandbox-2", Requests: model.ResourceAmounts{CPUMilli: 125}, Limits: model.ResourceAmounts{CPUMilli: 250}},
			{Namespace: "sandbox-3", Requests: model.ResourceAmounts{CPUMilli: 600}},
			{Namespace: "sandbox-40", Requests: model.ResourceAmounts{CPUMilli: 50}},
			{Namespace: "kube-system", Requests: model.ResourceAmounts{CPUMilli: 25}},
		},
		PVCs:              []model.ResourcePVC{{Namespace: "control", Bytes: 10}, {Namespace: "control", Bytes: 20}, {Namespace: "sandbox-2", Bytes: 7}, {Namespace: "sandbox-41", Bytes: 9}, {Namespace: "other-infra", Bytes: 13}},
		Workloads:         []model.ResourceWorkload{{Namespace: "sandbox-42", Name: "not-landed", Replicas: 10, Containers: []model.ResourceContainer{{Requests: model.ResourceAmounts{CPUMilli: 1000}}}}},
		NamespaceReserved: map[string]model.ResourceAmounts{"control": {CPUMilli: 75}, "sandbox-2": {CPUMilli: 375}},
	}
	units := BuildAllocationUnits(snapshot, "control")
	if len(units) != 8 || units[0].ID != "platform:control" || units[len(units)-1].ID != "infrastructure" {
		t.Fatalf("lost an allocation or unstable category ordering: %+v", units)
	}
	byID := map[string]model.ResourceAllocationUnit{}
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	platform := byID["platform:control"]
	if platform.Requested.CPUMilli != 250 || platform.Limits.CPUMilli != 500 || platform.PodCount != 2 || platform.Reserved.CPUMilli != 75 || platform.StorageBytes != 30 {
		t.Fatalf("rollout pods or PVCs counted incorrectly: %+v", platform)
	}
	queued := byID["sandbox:1"]
	if queued.Status != model.StatusQueued || queued.UserID != 7 || queued.ProjectID != 8 || queued.TemplateSlug != "mall" || queued.AllocationReason != "waiting" || queued.ResourcesAdmitted || queued.Requested.CPUMilli != 0 || queued.Reserved.CPUMilli != 0 || queued.StorageBytes != 0 || queued.ResourcePlan == nil || queued.ResourcePlan.Requests.CPUMilli != 900 || queued.ResourcePlan.StorageBytes != 1000 || queued.CreatedAt == nil || !queued.CreatedAt.Equal(at) {
		t.Fatalf("queued plan was lost or presented as actual occupation: %+v", queued)
	}
	admitted := byID["sandbox:2"]
	if admitted.Requested.CPUMilli != 125 || admitted.Reserved.CPUMilli != 375 || admitted.Limits.CPUMilli != 250 || admitted.StorageBytes != 7 || !admitted.ResourcesAdmitted {
		t.Fatalf("observed pods and unlanded reservation must remain separate: %+v", admitted)
	}
	if unit := byID["sandbox:3"]; unit.Requested.CPUMilli != 600 || unit.Reserved.CPUMilli != 0 {
		t.Fatalf("landed requests must not be charged again: %+v", unit)
	}
	for _, ns := range []string{"sandbox-40", "sandbox-41", "sandbox-42"} {
		unit, ok := byID["sandbox:"+ns]
		if !ok || unit.Namespace != ns || unit.SessionID != 0 || unit.UserID != 0 || unit.ResourcePlan != nil {
			t.Fatalf("orphan %s was lost or assigned an invented owner: %+v", ns, unit)
		}
	}
	if byID["sandbox:sandbox-42"].Requested.CPUMilli != 0 {
		t.Fatal("desired workload replicas must not count as observed requests")
	}
	if infra := byID["infrastructure"]; infra.Requested.CPUMilli != 25 || infra.StorageBytes != 13 {
		t.Fatalf("infrastructure across namespaces must stay one unit: %+v", infra)
	}
}

func TestAllocationUnitUsageRequiresEveryPodToHaveFreshMetrics(t *testing.T) {
	at := time.Now().UTC()
	metric := func(cpu int64, timestamp time.Time) *model.ResourceUsage {
		return &model.ResourceUsage{CPU: model.ResourceAmounts{CPUMilli: cpu, MemoryBytes: cpu * 10}, Timestamp: timestamp.Format(time.RFC3339Nano)}
	}
	for _, tc := range []struct {
		name        string
		metrics     []*model.ResourceUsage
		wantSampled int
		wantUsage   bool
	}{
		{"complete", []*model.ResourceUsage{metric(10, at.Add(-time.Second)), metric(20, at)}, 2, true},
		{"missing", []*model.ResourceUsage{metric(10, at), nil}, 1, false},
		{"stale", []*model.ResourceUsage{metric(10, at), metric(20, at.Add(-3*time.Minute))}, 1, false},
		{"future", []*model.ResourceUsage{metric(10, at), metric(20, at.Add(time.Minute))}, 1, false},
		{"invalid", []*model.ResourceUsage{{Timestamp: "invalid"}, nil}, 0, false},
		{"empty", nil, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := model.PoolSnapshot{ObservedAt: at}
			for _, usage := range tc.metrics {
				snapshot.Pods = append(snapshot.Pods, model.ResourcePod{Namespace: "control", Usage: usage})
			}
			unit := BuildAllocationUnits(snapshot, "control")[0]
			if unit.UsageSampledPods != tc.wantSampled || (unit.Usage != nil) != tc.wantUsage {
				t.Fatalf("partial or stale samples presented as a total: %+v", unit)
			}
			if tc.wantUsage && (unit.Usage.CPU.CPUMilli != 30 || unit.Usage.CPU.MemoryBytes != 300 || unit.Usage.Timestamp != at.Add(-time.Second).Format(time.RFC3339Nano)) {
				t.Fatalf("full aggregate must sum metrics and retain oldest sample timestamp: %+v", unit.Usage)
			}
		})
	}
}
