package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/repository"
	"testing"
)

type testScalingGuard struct {
	events                   *[]string
	failAcquire, failRelease bool
}

func (g testScalingGuard) Check(context.Context, string) error {
	if g.failAcquire {
		return errors.New("unavailable")
	}
	return nil
}

func (g testScalingGuard) Acquire(context.Context, string, string, string) error {
	*g.events = append(*g.events, "freeze")
	if g.failAcquire {
		return errors.New("unavailable")
	}
	return nil
}
func (g testScalingGuard) Release(context.Context, string, string) error {
	*g.events = append(*g.events, "release")
	if g.failRelease {
		return errors.New("unavailable")
	}
	return nil
}

type guardExecutor struct {
	events *[]string
	fail   bool
}

func (e guardExecutor) Run(_ context.Context, _ model.InjectSpec, args []string) (*BladeExecResult, error) {
	*e.events = append(*e.events, args[0])
	if e.fail {
		return nil, errors.New("unknown execution")
	}
	return &BladeExecResult{Raw: `{"success":true,"code":200,"result":"abc123"}`}, nil
}
func TestResourceFaultFreezesBeforeExecution(t *testing.T) {
	events := []string{}
	in := NewInjector(guardExecutor{events: &events})
	in.SetAutoscalingGuard(testScalingGuard{events: &events})
	spec := PrepareScalingSpec(model.InjectSpec{Kind: model.KindCPU, CPUPercent: 80, TargetNamespace: "sandbox-1"}, false)
	exp, e := in.Inject(context.Background(), spec)
	if e != nil || exp.Spec.ScalingHoldID == "" {
		t.Fatal(exp, e)
	}
	if len(events) != 2 || events[0] != "freeze" || events[1] != "create" {
		t.Fatal(events)
	}
	if e = in.Revoke(context.Background(), spec, exp.ExperimentUID); e != nil {
		t.Fatal(e)
	}
	if len(events) != 4 || events[2] != "destroy" || events[3] != "release" {
		t.Fatal(events)
	}
}
func TestResourceFaultUnavailableGuardNeverExecutes(t *testing.T) {
	events := []string{}
	in := NewInjector(guardExecutor{events: &events})
	in.SetAutoscalingGuard(testScalingGuard{events: &events, failAcquire: true})
	_, e := in.Inject(context.Background(), PrepareScalingSpec(model.InjectSpec{Kind: model.KindMem, MemPercent: 50, TargetNamespace: "sandbox-1"}, false))
	if e == nil || len(events) != 1 {
		t.Fatal(e, events)
	}
}
func TestResourceFaultUnknownCleanupKeepsHold(t *testing.T) {
	events := []string{}
	in := NewInjector(guardExecutor{events: &events, fail: true})
	in.SetAutoscalingGuard(testScalingGuard{events: &events})
	spec := PrepareScalingSpec(model.InjectSpec{Kind: model.KindCPU, CPUPercent: 80, TargetNamespace: "sandbox-1"}, false)
	if _, e := in.Inject(context.Background(), spec); e == nil {
		t.Fatal("unknown injection accepted")
	}
	if e := in.Revoke(context.Background(), spec, "abc123"); e == nil {
		t.Fatal("unknown cleanup accepted")
	}
	for _, v := range events {
		if v == "release" {
			t.Fatal(events)
		}
	}
}
func TestResourceCatalogClassAndIndependentIDs(t *testing.T) {
	a := PrepareScalingSpec(model.InjectSpec{Kind: model.KindNetDelay}, true)
	b := PrepareScalingSpec(model.InjectSpec{Kind: model.KindNetDelay}, true)
	if a.ScalingHoldID == "" || a.ScalingHoldID == b.ScalingHoldID || !a.FreezeAutoscaling {
		t.Fatal(a, b)
	}
	if PrepareScalingSpec(a, true).ScalingHoldID != a.ScalingHoldID {
		t.Fatal("unstable identity")
	}
}

func TestFaultCommandGuardFailureRejectsBeforeReservation(t *testing.T) {
	s, c, executor := faultCommandFixture(t, false)
	events := []string{}
	s.runtime.SetAutoscalingGuard(testScalingGuard{events: &events, failAcquire: true})
	_, e := s.Submit(context.Background(), c)
	if !errors.Is(e, ErrFaultCommandRejected) || executor.calls != 0 || s.store.(*memoryFaultCommands).exists {
		t.Fatal("guard failure became unknown reserved execution", e, executor.calls)
	}
}

type persistedScalingGuard struct {
	runtime  *RuntimeService
	t        *testing.T
	acquires int
	held     map[string]bool
}

func (g *persistedScalingGuard) Check(context.Context, string) error { return nil }
func (g *persistedScalingGuard) Acquire(ctx context.Context, ns, id, reason string) error {
	g.acquires++
	experiments, e := g.runtime.ListExperiments(ctx, 7)
	if e != nil {
		g.t.Fatal(e)
	}
	for _, exp := range experiments {
		if exp.Spec.ScalingHoldID == id {
			if g.held == nil {
				g.held = map[string]bool{}
			}
			g.held[id] = true
			return nil
		}
	}
	return errors.New("freeze acquired before its owning experiment was persisted")
}
func (g *persistedScalingGuard) Release(_ context.Context, _ string, id string) error {
	delete(g.held, id)
	return nil
}
func TestFaultCommandPersistsOwnerBeforeAcquiringHold(t *testing.T) {
	s, c, executor := faultCommandFixture(t, false)
	guard := &persistedScalingGuard{runtime: s.runtime, t: t}
	s.runtime.SetAutoscalingGuard(guard)
	result, e := s.Submit(context.Background(), c)
	if e != nil || result.Status != "succeeded" || executor.calls != 1 || guard.acquires != 1 {
		t.Fatal(result, e, executor.calls, guard.acquires)
	}
}

type failExperimentCreate struct {
	repository.ExperimentRepository
	calls, failAt int
}

func (r *failExperimentCreate) Create(ctx context.Context, e *model.Experiment) error {
	r.calls++
	if r.calls == r.failAt {
		return errors.New("experiment create unavailable")
	}
	return r.ExperimentRepository.Create(ctx, e)
}

type verifiedComboStore struct {
	memoryFaultCommands
	combo model.FaultCombination
}

func (s *verifiedComboStore) Combination(context.Context, string, []int64) (model.FaultCombination, error) {
	return s.combo, nil
}
func TestFaultCreateFailureDoesNotLeaveUnownedHolds(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			s, c, _ := faultCommandFixture(t, false)
			ctx := context.Background()
			if failAt == 2 {
				first, e := s.runtime.defectRepo.GetByID(ctx, c.DefectIDs[0])
				if e != nil {
					t.Fatal(e)
				}
				second := *first
				second.ID = 0
				second.Code = "second"
				if e = s.runtime.defectRepo.(interface {
					Create(context.Context, *model.Defect) error
				}).Create(ctx, &second); e != nil {
					t.Fatal(e)
				}
				saved, e := s.runtime.defectRepo.GetByID(ctx, second.ID)
				if e != nil {
					t.Fatal(e)
				}
				c.DefectIDs = append(c.DefectIDs, saved.ID)
				c.AcceptanceDigest = FaultAcceptanceDigest("revision", []*model.Defect{first, saved})
				s.store = &verifiedComboStore{combo: model.FaultCombination{TemplateSlug: "mall", DefectIDs: c.DefectIDs, Status: "verified", AcceptanceDigest: c.AcceptanceDigest, EvidenceJSON: `{"baseline":"b","combined":"c","recovery":"r","isolation":"i","source":"s"}`}}
			}
			s.runtime.expRepo = &failExperimentCreate{ExperimentRepository: s.runtime.expRepo, failAt: failAt}
			guard := &persistedScalingGuard{runtime: s.runtime, t: t}
			s.runtime.SetAutoscalingGuard(guard)
			result, e := s.Submit(ctx, c)
			if e != nil || result.Status != "unknown" {
				t.Fatal(result, e)
			}
			if len(guard.held) != 0 || guard.acquires != failAt-1 {
				t.Fatal("unowned/unused hold remained", guard.held, guard.acquires)
			}
		})
	}
}
