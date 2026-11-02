package service

import (
	"context"
	"github.com/quenching/sandbox-service/internal/model"
	"testing"
)

type allocationFake struct {
	Store
	items  []model.Sandbox
	starts int
}

func (f *allocationFake) WithAllocationLock(c context.Context, fn func(context.Context) error) error {
	return fn(c)
}
func (f *allocationFake) AllocationCandidates(context.Context) ([]model.Sandbox, error) {
	return f.items, nil
}
func (f *allocationFake) ListReservations(context.Context) ([]model.Sandbox, error) {
	return f.items, nil
}
func (f *allocationFake) Admit(_ context.Context, id int64) (bool, error) {
	for i := range f.items {
		if f.items[i].SessionID == id {
			f.items[i].ResourcesAdmitted = true
			f.items[i].Status = model.StatusDeploying
			return true, nil
		}
	}
	return false, nil
}
func (f *allocationFake) SetAllocationReason(context.Context, int64, string) error { return nil }
func TestQueueWaitsThenStarts(t *testing.T) {
	p := model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 1000}, Components: []model.ResourceComponent{}}
	f := &allocationFake{items: []model.Sandbox{{SessionID: 1, Status: model.StatusQueued, ResourcePlan: p}, {SessionID: 2, Status: model.StatusQueued, ResourcePlan: p}}}
	snap := model.PoolSnapshot{NamespaceRequests: map[string]model.ResourceAmounts{}}
	q := ResourceQueue{Store: f, Snapshot: func(context.Context) (model.PoolSnapshot, error) { return snap, nil }, Deploy: func(context.Context, model.Sandbox) error { f.starts++; return nil }}
	if e := q.Tick(context.Background()); e != nil || f.starts != 0 {
		t.Fatal(e, f.starts)
	}
	snap.Available = model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 1000}
	if e := q.Tick(context.Background()); e != nil || f.starts != 1 || f.items[1].ResourcesAdmitted {
		t.Fatal(e, f.starts, f.items)
	}
}
func TestQueueCannotRestartClosed(t *testing.T) {
	f := &allocationFake{items: []model.Sandbox{{SessionID: 1, Status: model.StatusReleased, ResourcesAdmitted: true}}}
	q := ResourceQueue{Store: f, Snapshot: func(context.Context) (model.PoolSnapshot, error) { return model.PoolSnapshot{}, nil }, Deploy: func(context.Context, model.Sandbox) error { f.starts++; return nil }}
	if e := q.Tick(context.Background()); e != nil || f.starts != 0 {
		t.Fatal(e, f.starts)
	}
}
