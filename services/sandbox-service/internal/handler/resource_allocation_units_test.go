package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type allocationSnapshotStore struct {
	service.ElasticStore
	allocations []model.Sandbox
	state       model.ElasticState
}

func (s allocationSnapshotStore) ListActive(context.Context) ([]model.Sandbox, error) {
	return s.allocations, nil
}

func (s allocationSnapshotStore) ElasticState(context.Context) (model.ElasticState, error) {
	return s.state, nil
}

func TestResourceSnapshotSerializesAllocationUnitsWithoutChargingReservationsTwice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller := true
	pod := func(namespace, name, cpu, memory string) *core.Pod {
		return &core.Pod{ObjectMeta: meta.ObjectMeta{Namespace: namespace, Name: name}, Spec: core.PodSpec{NodeName: "node", Containers: []core.Container{{Name: "app", Resources: core.ResourceRequirements{
			Requests: core.ResourceList{core.ResourceCPU: resource.MustParse(cpu), core.ResourceMemory: resource.MustParse(memory)},
			Limits:   core.ResourceList{core.ResourceCPU: resource.MustParse("1"), core.ResourceMemory: resource.MustParse("1000")},
		}}}}, Status: core.PodStatus{Phase: core.PodRunning}}
	}
	platformPod := pod("control", "platform", "100m", "100")
	platformPod.OwnerReferences = []meta.OwnerReference{{Kind: "ReplicaSet", UID: "rs", Controller: &controller}}
	client := fake.NewSimpleClientset(
		&core.Node{ObjectMeta: meta.ObjectMeta{Name: "node"}, Status: core.NodeStatus{Allocatable: core.ResourceList{core.ResourceCPU: resource.MustParse("4"), core.ResourceMemory: resource.MustParse("8192")}, Conditions: []core.NodeCondition{{Type: core.NodeReady, Status: core.ConditionTrue}}}},
		&apps.Deployment{ObjectMeta: meta.ObjectMeta{Name: "api", Namespace: "control", UID: "api-uid"}},
		&apps.ReplicaSet{ObjectMeta: meta.ObjectMeta{Name: "api-rs", Namespace: "control", UID: "rs", OwnerReferences: []meta.OwnerReference{{Kind: "Deployment", UID: "api-uid", Controller: &controller}}}},
		platformPod, pod("control", "unrelated", "50m", "50"),
		pod("sandbox-2", "deploying", "150m", "100"), pod("sandbox-3", "landed", "250m", "150"),
		&core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Namespace: "sandbox-9", Name: "orphan-pvc"}, Spec: core.PersistentVolumeClaimSpec{Resources: core.VolumeResourceRequirements{Requests: core.ResourceList{core.ResourceStorage: resource.MustParse("70")}}}},
	)
	store := allocationSnapshotStore{
		allocations: []model.Sandbox{
			{SessionID: 1, Namespace: "sandbox-1", Status: model.StatusQueued, ResourcePlan: model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 999, MemoryBytes: 999}}},
			{SessionID: 2, Namespace: "sandbox-2", Status: model.StatusDeploying, ResourcesAdmitted: true, ResourcePlan: model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 500, MemoryBytes: 500}}},
			{SessionID: 3, Namespace: "sandbox-3", Status: model.StatusReady, ResourcesAdmitted: true, ResourcePlan: model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 200, MemoryBytes: 100}}},
		},
		state: model.ElasticState{Policies: []model.ElasticPolicy{
			{Namespace: "control", Name: "api", UID: "api-uid", Pending: &model.ElasticIntent{Previous: model.ElasticTarget{Replicas: 1, Requests: model.ResourceAmounts{CPUMilli: 100, MemoryBytes: 100}}, Target: model.ElasticTarget{Replicas: 2, Requests: model.ResourceAmounts{CPUMilli: 100, MemoryBytes: 100}}}},
			{Namespace: "sandbox-2", Name: "web", SessionID: 2, Pending: &model.ElasticIntent{Previous: model.ElasticTarget{Replicas: 1, Requests: model.ResourceAmounts{CPUMilli: 100}}, Target: model.ElasticTarget{Replicas: 3, Requests: model.ResourceAmounts{CPUMilli: 100}}}},
		}, Holds: []model.ScalingHold{{Namespace: "sandbox-2", ID: "fault", Reason: "CPU fault"}}},
	}
	pool := service.ResourcePool{Client: client, PlatformNamespace: "control"}
	h := ResourcesHandler{Pool: service.ResourcePool{PlatformNamespace: "unused-handler-config"}, Elastic: &service.ElasticController{Pool: pool, Store: store}, Store: store}
	r := gin.New()
	r.GET("/snapshot", h.snapshot)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/snapshot", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot failed: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data model.PoolSnapshot `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	snapshot := body.Data
	if len(snapshot.AllocationUnits) != 6 {
		t.Fatalf("HTTP response lost active or orphan allocations: %s", w.Body.String())
	}
	units := map[string]model.ResourceAllocationUnit{}
	reserved := model.ResourceAmounts{}
	for _, unit := range snapshot.AllocationUnits {
		units[unit.ID] = unit
		reserved = reserved.Add(unit.Reserved)
	}
	if reserved != snapshot.Reserved || reserved.CPUMilli != 450 || reserved.MemoryBytes != 500 || snapshot.Available.CPUMilli != 3000 || snapshot.Available.MemoryBytes != 7292 {
		t.Fatalf("projection changed the existing admission budget or lost a reservation: %+v", snapshot)
	}
	if platform := units["platform:control"]; platform.Requested.CPUMilli != 150 || platform.Limits.CPUMilli != 2000 || platform.PodCount != 2 || platform.Reserved.CPUMilli != 100 {
		t.Fatalf("configured pool namespace or UID-owned platform reserve was lost: %+v", platform)
	}
	if queued := units["sandbox:1"]; queued.Requested.CPUMilli != 0 || queued.Reserved.CPUMilli != 0 || queued.ResourcePlan == nil || queued.ResourcePlan.Requests.CPUMilli != 999 || queued.Usage != nil {
		t.Fatalf("queued plan was incorrectly counted as occupied: %+v", queued)
	}
	if deploying := units["sandbox:2"]; deploying.Requested.CPUMilli != 150 || deploying.Reserved.CPUMilli != 350 {
		t.Fatalf("sandbox elastic intent was double counted with its persisted budget: %+v", deploying)
	}
	if units["sandbox:3"].Reserved.CPUMilli != 0 || units["sandbox:sandbox-9"].StorageBytes != 70 {
		t.Fatal("landed reservation or orphan PVC is incorrect", units)
	}
	if snapshot.Elastic == nil || !snapshot.Elastic.Policies[1].Frozen {
		t.Fatal("resource fault freezing was lost in the grouped response")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &struct {
		Data *map[string]json.RawMessage `json:"data"`
	}{Data: &raw}); err != nil {
		t.Fatal(err)
	}
	if _, exposed := raw["NamespaceReserved"]; exposed {
		t.Fatal("internal reservation attribution leaked into the public contract")
	}
	var rawUnits []map[string]json.RawMessage
	if err := json.Unmarshal(raw["allocation_units"], &rawUnits); err != nil {
		t.Fatal(err)
	}
	if string(rawUnits[0]["usage"]) != "null" || string(rawUnits[0]["usage_sampled_pods"]) != "0" {
		t.Fatal("unavailable metrics must serialize as null with explicit coverage")
	}
}
