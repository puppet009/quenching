package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/quenching/sandbox-service/internal/model"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"sync"
	"testing"
	"time"
)

type memoryElasticStore struct {
	mu       sync.Mutex
	state    model.ElasticState
	sb       *model.Sandbox
	failSave bool
}

func (s *memoryElasticStore) WithElasticLock(ctx context.Context, _ bool, f func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return f(ctx)
}
func (s *memoryElasticStore) WithAllocationLock(ctx context.Context, f func(context.Context) error) error {
	return f(ctx)
}
func (s *memoryElasticStore) ElasticState(context.Context) (model.ElasticState, error) {
	return s.state, nil
}
func (s *memoryElasticStore) InsertElasticPolicy(_ context.Context, p model.ElasticPolicy) error {
	for _, old := range s.state.Policies {
		if old.Namespace == p.Namespace && old.Name == p.Name {
			return nil
		}
	}
	p.Version = 1
	s.state.Policies = append(s.state.Policies, p)
	return nil
}
func (s *memoryElasticStore) SaveElasticPolicy(_ context.Context, p *model.ElasticPolicy, b *model.ResourcePlan) error {
	if s.failSave {
		return errors.New("db failure")
	}
	p.Version++
	for i := range s.state.Policies {
		if s.state.Policies[i].Name == p.Name && s.state.Policies[i].Namespace == p.Namespace {
			s.state.Policies[i] = *p
		}
	}
	if b != nil {
		s.sb.ResourcePlan = *b
	}
	return nil
}
func (s *memoryElasticStore) AcquireScalingHold(_ context.Context, h model.ScalingHold) error {
	for _, old := range s.state.Holds {
		if old.Namespace == h.Namespace && old.ID == h.ID {
			return nil
		}
	}
	s.state.Holds = append(s.state.Holds, h)
	return nil
}
func (s *memoryElasticStore) ReleaseScalingHold(_ context.Context, ns, id string) error {
	out := []model.ScalingHold{}
	for _, h := range s.state.Holds {
		if h.Namespace != ns || h.ID != id {
			out = append(out, h)
		}
	}
	s.state.Holds = out
	return nil
}
func (s *memoryElasticStore) DeleteElasticNamespace(context.Context, string) error { return nil }
func (s *memoryElasticStore) AuditElastic(_ context.Context, a model.ElasticAudit) error {
	s.state.Audit = append(s.state.Audit, a)
	return nil
}
func (s *memoryElasticStore) ListReservations(context.Context) ([]model.Sandbox, error) {
	if s.sb != nil {
		return []model.Sandbox{*s.sb}, nil
	}
	return nil, nil
}
func (s *memoryElasticStore) GetBySessionID(context.Context, int64) (*model.Sandbox, error) {
	return s.sb, nil
}

func elasticFixture() (*ElasticController, *memoryElasticStore, *apps.Deployment) {
	p := testPolicy()
	p.Namespace = "sandbox-1"
	p.Name = "frontend"
	p.UID = "uid1"
	p.SessionID = 1
	p.Component = "frontend"
	p.Version = 1
	replicas := int32(1)
	d := &apps.Deployment{ObjectMeta: meta.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: types.UID(p.UID), ResourceVersion: "1"}, Spec: apps.DeploymentSpec{Replicas: &replicas, Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}}, Template: core.PodTemplateSpec{Spec: core.PodSpec{Containers: []core.Container{{Name: "frontend", Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("1000")}, Limits: core.ResourceList{core.ResourceCPU: resource.MustParse("200m"), core.ResourceMemory: resource.MustParse("2000")}}}}}}}, Status: apps.DeploymentStatus{Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}}
	sb := &model.Sandbox{SessionID: 1, Namespace: p.Namespace, ResourcesAdmitted: true, Status: model.StatusReady, ResourcePlan: model.ResourcePlan{Requests: p.Min, Limits: p.Limits, Components: []model.ResourceComponent{{Name: "frontend", Requests: p.Min}}}}
	s := &memoryElasticStore{state: model.ElasticState{Policies: []model.ElasticPolicy{p}}, sb: sb}
	c := &ElasticController{Client: fake.NewSimpleClientset(d, &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: p.Namespace}}, BuildPlannedResourceQuota(p.Namespace, sb.ResourcePlan)), Store: s, Pool: ResourcePool{PlatformNamespace: "platform"}, CheckExternal: func(context.Context, string) error { return nil }}
	return c, s, d
}
func TestElasticMultipleHoldsAndRestart(t *testing.T) {
	c, s, _ := elasticFixture()
	ctx := context.Background()
	for _, id := range []string{"one", "two", "one"} {
		if e := c.AcquireHold(ctx, model.ScalingHold{Namespace: "sandbox-1", ID: id, Reason: "cpu"}); e != nil {
			t.Fatal(e)
		}
	}
	if len(s.state.Holds) != 2 {
		t.Fatal(s.state.Holds)
	}
	restarted := &ElasticController{Client: c.Client, Store: s, Pool: c.Pool, CheckExternal: c.CheckExternal}
	if e := restarted.ReleaseHold(ctx, "sandbox-1", "one"); e != nil {
		t.Fatal(e)
	}
	if len(s.state.Holds) != 1 {
		t.Fatal("second fault lost")
	}
	if e := restarted.ReleaseHold(ctx, "sandbox-1", "two"); e != nil || len(s.state.Holds) != 0 {
		t.Fatal(e)
	}
	if s.state.Policies[0].LastChanged.IsZero() {
		t.Fatal("no post-fault cooldown")
	}
}
func TestElasticPendingIntentPreventsFaultAndReservesCapacity(t *testing.T) {
	c, s, _ := elasticFixture()
	p := &s.state.Policies[0]
	p.SessionID = 0
	p.Pending = &model.ElasticIntent{ID: "resize", Previous: p.Current, Target: model.ElasticTarget{Replicas: 2, Requests: p.Min}}
	c.SnapshotBase = func(context.Context) (model.PoolSnapshot, error) {
		return model.PoolSnapshot{Available: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}, Pods: []model.ResourcePod{{Namespace: p.Namespace, WorkloadUID: p.UID, Requests: p.Min}}}, nil
	}
	snap, e := c.Snapshot(context.Background())
	if e != nil || snap.Available.CPUMilli != 900 {
		t.Fatal(snap, e)
	}
	if snap.NamespaceReserved[p.Namespace].CPUMilli != 100 {
		t.Fatal("pending platform capacity lost its allocation namespace", snap)
	}
	p.SessionID = 1
	if c.AcquireHold(context.Background(), model.ScalingHold{Namespace: "sandbox-1", ID: "one", Reason: "cpu"}) == nil {
		t.Fatal("fault started in rollout")
	}
	if len(s.state.Holds) != 0 {
		t.Fatal("left hold for rejected fault")
	}
}
func TestElasticResizePersistsBeforeMutationAndUsesZeroSurge(t *testing.T) {
	c, s, d := elasticFixture()
	ctx := context.Background()
	p := s.state.Policies[0]
	target := p.Current
	target.Requests.CPUMilli = 125
	snap := model.PoolSnapshot{Available: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}, NamespaceRequests: map[string]model.ResourceAmounts{p.Namespace: p.Min}, Nodes: []model.ResourceNode{{Eligible: true, Allocatable: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}}}}
	s.failSave = true
	if c.BeginResize(ctx, &p, d, target, snap) == nil {
		t.Fatal("save failure accepted")
	}
	got, _ := c.Client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Name, meta.GetOptions{})
	if amounts(got.Spec.Template.Spec.Containers[0].Resources.Requests).CPUMilli != 100 {
		t.Fatal("changed before reservation")
	}
	s.failSave = false
	if e := c.BeginResize(ctx, &p, d, target, snap); e != nil {
		t.Fatal(e)
	}
	got, _ = c.Client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Name, meta.GetOptions{})
	if p.Pending == nil || s.sb.ResourcePlan.Requests.CPUMilli != 125 || got.Spec.Strategy.RollingUpdate.MaxSurge.IntVal != 0 {
		t.Fatal("missing durable budget or surge guard")
	}
	q, _ := c.Client.CoreV1().ResourceQuotas(p.Namespace).Get(ctx, BuildPlannedResourceQuota(p.Namespace, s.sb.ResourcePlan).Name, meta.GetOptions{})
	cpu := q.Spec.Hard[core.ResourceRequestsCPU]
	if cpu.MilliValue() != 125 {
		t.Fatal(q.Spec.Hard)
	}
}
func TestElasticCapacityRefusalAndFreezeBlockNewResize(t *testing.T) {
	c, s, d := elasticFixture()
	p := s.state.Policies[0]
	target := p.Current
	target.Replicas = 2
	if c.BeginResize(context.Background(), &p, d, target, model.PoolSnapshot{}) == nil {
		t.Fatal("no capacity accepted")
	}
	if p.Pending != nil {
		t.Fatal("reserved refused capacity")
	}
	s.state.Holds = []model.ScalingHold{{Namespace: p.Namespace, ID: "held"}}
	if c.BeginResize(context.Background(), &p, d, target, model.PoolSnapshot{Available: model.ResourceAmounts{CPUMilli: 10000, MemoryBytes: 10000}}) == nil {
		t.Fatal("frozen namespace scaled")
	}
}
func TestElasticInvalidSamplesResetHysteresis(t *testing.T) {
	p := testPolicy()
	now := time.Now()
	high := model.ResourceAmounts{CPUMilli: 90, MemoryBytes: 800}
	DecideElastic(&p, high, now)
	ResetElasticSamples(&p)
	if DecideElastic(&p, high, now.Add(30*time.Second)) != nil {
		t.Fatal("missing metric did not reset")
	}
}

func TestElasticIntentSurvivesStatusOnlyVersionChange(t *testing.T) {
	c, s, d := elasticFixture()
	ctx := context.Background()
	p := s.state.Policies[0]
	target := p.Current
	target.Requests.CPUMilli = 125
	p.Pending = &model.ElasticIntent{ID: "saved-before-crash", Version: d.ResourceVersion, Previous: p.Current, Target: target}
	d.ResourceVersion = "status-2"
	if _, e := c.Client.AppsV1().Deployments(d.Namespace).Update(ctx, d, meta.UpdateOptions{}); e != nil {
		t.Fatal(e)
	}
	if e := c.applyIntent(ctx, &p, d); e != nil {
		t.Fatalf("status-only change stranded intent: %v", e)
	}
	got, _ := c.Client.AppsV1().Deployments(d.Namespace).Get(ctx, d.Name, meta.GetOptions{})
	if deploymentTarget(got) != target {
		t.Fatal("did not resume persisted intent")
	}
}
func TestElasticPlatformReservationSurvivesNoPodGap(t *testing.T) {
	c, s, _ := elasticFixture()
	p := &s.state.Policies[0]
	p.SessionID = 0
	p.Pending = &model.ElasticIntent{ID: "rollout", Previous: p.Current, Target: model.ElasticTarget{Replicas: 1, Requests: model.ResourceAmounts{CPUMilli: 125, MemoryBytes: 1250}}}
	c.SnapshotBase = func(context.Context) (model.PoolSnapshot, error) {
		return model.PoolSnapshot{Available: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}}, nil
	}
	snap, e := c.Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if snap.Available.CPUMilli != 875 || snap.Reserved.CPUMilli != 125 {
		t.Fatalf("baseline reservation disappeared with old Pod: %+v", snap)
	}
	if ok, _ := CanAdmit(snap, model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 900, MemoryBytes: 1}}, nil); ok {
		t.Fatal("queue consumed baseline rollout reservation")
	}
}

func TestElasticPreflightHasNoPersistentMutation(t *testing.T) {
	c, s, _ := elasticFixture()
	if e := c.CheckHold(context.Background(), "sandbox-1"); e != nil {
		t.Fatal(e)
	}
	if len(s.state.Holds) != 0 || len(s.state.Audit) != 0 {
		t.Fatal("read-only preflight left state after a potential crash")
	}
}
func TestElasticIntentRejectsActualSpecGenerationChange(t *testing.T) {
	c, s, d := elasticFixture()
	p := s.state.Policies[0]
	target := p.Current
	target.Requests.CPUMilli = 125
	p.Pending = &model.ElasticIntent{ID: "old", Version: d.ResourceVersion, Generation: d.Generation, Previous: p.Current, Target: target}
	d.Generation++
	d.ResourceVersion = "2"
	if e := c.applyIntent(context.Background(), &p, d); !errors.Is(e, ErrResourceConflict) {
		t.Fatal("overwrote an external spec change", e)
	}
}

func TestElasticResourceFaultAlsoFreezesFaultEngineRestart(t *testing.T) {
	c, s, d := elasticFixture()
	ctx := context.Background()
	p := s.state.Policies[0]
	p.Namespace = "platform"
	p.Name = "fault-engine"
	p.SessionID = 0
	s.state.Policies[0] = p
	d.Namespace = p.Namespace
	d.Name = p.Name
	c.Client.AppsV1().Deployments(p.Namespace).Create(ctx, d, meta.CreateOptions{})
	s.state.Holds = []model.ScalingHold{{Namespace: "sandbox-1", ID: "cpu"}}
	target := p.Current
	target.Requests.CPUMilli = 125
	snap := model.PoolSnapshot{Available: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}, NamespaceRequests: map[string]model.ResourceAmounts{"sandbox-1": p.Min}, Nodes: []model.ResourceNode{{Eligible: true, Allocatable: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}}}}
	if e := c.BeginResize(ctx, &p, d, target, snap); e == nil {
		t.Fatal("fault-engine rollout would invoke StopAll and erase active training fault")
	}
	if p.Pending != nil {
		t.Fatal("reserved a forbidden restart")
	}
}
func TestElasticFaultWaitsForFaultEnginePendingRollout(t *testing.T) {
	c, s, _ := elasticFixture()
	s.state.Policies = append(s.state.Policies, model.ElasticPolicy{Namespace: "platform", Name: "fault-engine", Pending: &model.ElasticIntent{ID: "pending"}})
	if e := c.AcquireHold(context.Background(), model.ScalingHold{Namespace: "sandbox-1", ID: "cpu", Reason: "cpu"}); e == nil {
		t.Fatal("fault began before fault-engine replacement completed")
	}
	if len(s.state.Holds) != 0 {
		t.Fatal("left rejected hold")
	}
}

func TestElasticReservationDeductsOnlyOwnedObservedPods(t *testing.T) {
	c, s, d := elasticFixture()
	ctx := context.Background()
	p := &s.state.Policies[0]
	p.SessionID = 0
	p.Pending = &model.ElasticIntent{ID: "scaling", Previous: p.Current, Target: model.ElasticTarget{Replicas: 2, Requests: p.Min}}
	c.Pool.Client = c.Client
	owned := true
	rs := &apps.ReplicaSet{ObjectMeta: meta.ObjectMeta{Namespace: d.Namespace, Name: "rs", UID: "rs", OwnerReferences: []meta.OwnerReference{{Kind: "Deployment", UID: d.UID, Controller: &owned}}}}
	if _, e := c.Client.AppsV1().ReplicaSets(d.Namespace).Create(ctx, rs, meta.CreateOptions{}); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"owned", "foreign"} {
		pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Namespace: d.Namespace, Name: name}, Spec: d.Spec.Template.Spec, Status: core.PodStatus{Phase: core.PodRunning}}
		if name == "owned" {
			pod.OwnerReferences = []meta.OwnerReference{{Kind: "ReplicaSet", UID: rs.UID, Controller: &owned}}
		}
		if _, e := c.Client.CoreV1().Pods(d.Namespace).Create(ctx, pod, meta.CreateOptions{}); e != nil {
			t.Fatal(e)
		}
	}
	snap, e := c.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if snap.Reserved.CPUMilli != 100 {
		t.Fatalf("wrong attribution of owned/foreign Pod: %+v", snap)
	}
}

func TestElasticShrinkRetainsBudgetUntilQuotaConfirmed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			c, s, d := elasticFixture()
			ctx := context.Background()
			p := s.state.Policies[0]
			before := p.Current
			target := before
			target.Requests.CPUMilli = 80
			final := s.sb.ResourcePlan
			final.Requests = target.Requests
			budget := s.sb.ResourcePlan
			p.Pending = &model.ElasticIntent{ID: "shrinking", Previous: before, Target: target, Budget: &budget, FinalBudget: &final}
			s.state.Policies[0] = p
			d.Annotations = map[string]string{elasticAnnotation: p.Pending.ID}
			d.Spec.Template.Spec.Containers[0].Resources.Requests = resourceList(target.Requests)
			pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Namespace: d.Namespace, Name: "replacement", Labels: map[string]string{"app": "frontend"}}, Spec: d.Spec.Template.Spec, Status: core.PodStatus{Phase: core.PodRunning}}
			c.Client.CoreV1().Pods(d.Namespace).Create(ctx, pod, meta.CreateOptions{})
			if fail {
				c.Client.(*fake.Clientset).PrependReactor("update", "resourcequotas", func(a ktesting.Action) (bool, runtime.Object, error) {
					q := a.(ktesting.UpdateAction).GetObject().(*core.ResourceQuota)
					cpu := q.Spec.Hard[core.ResourceRequestsCPU]
					if cpu.MilliValue() == 80 {
						return true, nil, errors.New("quota update response lost")
					}
					return false, nil, nil
				})
			}
			e := c.applyIntent(ctx, &p, d)
			if fail && e == nil {
				t.Fatal("final quota failure ignored")
			}
			if p.Pending == nil || s.sb.ResourcePlan.Requests.CPUMilli != 100 {
				t.Fatal("released budget before final quota confirmed")
			}
			if !fail {
				quotas := c.Client.CoreV1().ResourceQuotas(d.Namespace)
				q, _ := quotas.Get(ctx, BuildPlannedResourceQuota(d.Namespace, final).Name, meta.GetOptions{})
				cpu := q.Spec.Hard[core.ResourceRequestsCPU]
				if cpu.MilliValue() != 80 {
					t.Fatal("final quota never submitted")
				}
				q.Status.Hard = q.Spec.Hard.DeepCopy()
				quotas.UpdateStatus(ctx, q, meta.UpdateOptions{})
				if e = c.applyIntent(ctx, &p, d); e != nil || p.Pending != nil || s.sb.ResourcePlan.Requests.CPUMilli != 80 {
					t.Fatal("confirmed quota did not release budget", e)
				}
			}
		})
	}
}
func TestElasticPreservesOtherResourceRequests(t *testing.T) {
	c, s, d := elasticFixture()
	p := s.state.Policies[0]
	p.SessionID = 0
	target := p.Current
	target.Requests.CPUMilli = 125
	d.Spec.Template.Spec.Containers[0].Resources.Requests[core.ResourceEphemeralStorage] = resource.MustParse("1Gi")
	p.Pending = &model.ElasticIntent{ID: "resize", Previous: p.Current, Target: target, Version: d.ResourceVersion}
	if e := c.applyIntent(context.Background(), &p, d); e != nil {
		t.Fatal(e)
	}
	got, _ := c.Client.AppsV1().Deployments(d.Namespace).Get(context.Background(), d.Name, meta.GetOptions{})
	disk := got.Spec.Template.Spec.Containers[0].Resources.Requests[core.ResourceEphemeralStorage]
	if disk.Cmp(resource.MustParse("1Gi")) != 0 {
		t.Fatal("removed request outside CPU/memory scope")
	}
}

func TestElasticFreezeAllowsSecondFaultOnUnhealthyDeployment(t *testing.T) {
	c, s, d := elasticFixture()
	ctx := context.Background()
	if e := c.AcquireHold(ctx, model.ScalingHold{Namespace: d.Namespace, ID: "first", Reason: "cpu"}); e != nil {
		t.Fatal(e)
	}
	d.Status.ReadyReplicas = 0
	_, e := c.Client.AppsV1().Deployments(d.Namespace).UpdateStatus(ctx, d, meta.UpdateOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.AcquireHold(ctx, model.ScalingHold{Namespace: d.Namespace, ID: "second", Reason: "memory"}); e != nil || len(s.state.Holds) != 2 {
		t.Fatal("compound fault refused", e)
	}
}

func TestElasticTickScalesAndRetainsIntentUntilPodsConverge(t *testing.T) {
	c, s, d := elasticFixture()
	ctx := context.Background()
	owned := true
	now := time.Now().UTC()
	rs := &apps.ReplicaSet{ObjectMeta: meta.ObjectMeta{Namespace: d.Namespace, Name: "frontend-rs", UID: "rs-uid", Labels: map[string]string{"app": "frontend"}, OwnerReferences: []meta.OwnerReference{{UID: d.UID, Controller: &owned}}}}
	pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Namespace: d.Namespace, Name: "frontend-1", Labels: map[string]string{"app": "frontend"}, OwnerReferences: []meta.OwnerReference{{UID: rs.UID, Controller: &owned}}}, Spec: d.Spec.Template.Spec, Status: core.PodStatus{Phase: core.PodRunning, Conditions: []core.PodCondition{{Type: core.PodReady, Status: core.ConditionTrue}}}}
	c.Client.AppsV1().ReplicaSets(d.Namespace).Create(ctx, rs, meta.CreateOptions{})
	c.Client.CoreV1().Pods(d.Namespace).Create(ctx, pod, meta.CreateOptions{})
	at := now.Add(-30 * time.Second)
	c.SnapshotBase = func(context.Context) (model.PoolSnapshot, error) {
		return model.PoolSnapshot{Available: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}, NamespaceRequests: map[string]model.ResourceAmounts{d.Namespace: PodResources(*pod, false)}, Nodes: []model.ResourceNode{{Eligible: true, Allocatable: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 10000}}}, Pods: []model.ResourcePod{{Namespace: d.Namespace, Name: pod.Name, Usage: &model.ResourceUsage{CPU: model.ResourceAmounts{CPUMilli: 90, MemoryBytes: 800}, Timestamp: at.Format(time.RFC3339Nano)}}}}, nil
	}
	if e := c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if s.state.Policies[0].Pending != nil {
		t.Fatal("first sample scaled")
	}
	at = now
	if e := c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if s.state.Policies[0].Pending == nil {
		t.Fatal("controller never scaled")
	}
	if e := c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if s.state.Policies[0].Pending == nil {
		t.Fatal("released before pods matched")
	}
	q, _ := c.Client.CoreV1().ResourceQuotas(d.Namespace).Get(ctx, BuildPlannedResourceQuota(d.Namespace, s.sb.ResourcePlan).Name, meta.GetOptions{})
	q.Status.Hard = q.Spec.Hard.DeepCopy()
	c.Client.CoreV1().ResourceQuotas(d.Namespace).UpdateStatus(ctx, q, meta.UpdateOptions{})
	pod.Spec.Containers[0].Resources.Requests = resourceList(s.state.Policies[0].Pending.Target.Requests)
	c.Client.CoreV1().Pods(d.Namespace).Update(ctx, pod, meta.UpdateOptions{})
	if e := c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if s.state.Policies[0].Pending != nil || s.state.Policies[0].Current.Requests.CPUMilli != 125 {
		t.Fatal("did not converge")
	}
}
