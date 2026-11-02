package service

import (
	"context"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	"testing"
	"time"
)

type sealFake struct {
	seal  oe.AuditSeal
	saved oe.AuditSeal
}

type freezeUnavailableSandbox struct{ cleanupTestSandbox }

func (freezeUnavailableSandbox) MarkCompleted(context.Context, int64, string) error {
	return errors.New("sandbox unavailable")
}

type unavailableRemoteSeal struct{}

func (unavailableRemoteSeal) SealOperations(context.Context, int64) (oe.AuditSeal, error) {
	return oe.AuditSeal{}, errors.New("sandbox unavailable")
}
func TestRemoteFreezeFailureStillTimesOutAndReports(t *testing.T) {
	local := &sealFake{seal: oe.AuditSeal{SessionID: 1, Producer: "training", State: "ready", Deadline: time.Now().Add(time.Minute)}}
	reports := &cleanupTestReview{}
	state := &cleanupTestStore{}
	c := NewBrainCleanupCoordinator(state, cleanupTestFault{}, freezeUnavailableSandbox{}, reports).WithOperationSeal(&OperationSealService{Local: local, Remote: unavailableRemoteSeal{}})
	_ = c.Close(context.Background(), 1)
	if local.saved.State != "pending" || reports.calls != 0 {
		t.Fatal("durable deadline never established", local.saved, reports.calls)
	}
	local.seal.Deadline = time.Now().Add(-time.Second)
	_ = c.Close(context.Background(), 1)
	if local.saved.State != "timed_out" || state.status != "failed" || reports.calls != 1 {
		t.Fatal(local.saved, state.status, reports.calls)
	}
}

func (f *sealFake) Seal(context.Context, int64, string, time.Time) (oe.AuditSeal, error) {
	return f.seal, nil
}
func (f *sealFake) SaveRemoteSeal(_ context.Context, _ int64, s oe.AuditSeal) error {
	f.saved = s
	return nil
}
func (f *sealFake) SealOperations(context.Context, int64) (oe.AuditSeal, error) { return f.seal, nil }
func TestCleanupAndAuditIndependent(t *testing.T) {
	now := time.Now()
	local := &sealFake{seal: oe.AuditSeal{State: "ready", Deadline: now.Add(time.Minute)}}
	remote := &sealFake{seal: oe.AuditSeal{SessionID: 1, Producer: "sandbox", State: "pending", Deadline: now.Add(time.Minute), Pending: 1}}
	review := &cleanupTestReview{}
	coordinator := NewBrainCleanupCoordinator(&cleanupTestStore{}, cleanupTestFault{}, cleanupTestSandbox{}, review).WithOperationSeal(&OperationSealService{Local: local, Remote: remote})
	if err := coordinator.Close(context.Background(), 1); err != nil || review.calls != 0 {
		t.Fatal(err, review.calls)
	}
	remote.seal.State = "timed_out"
	remote.seal.Gaps = []string{"audit_delivery_timeout"}
	if err := coordinator.Close(context.Background(), 1); err != nil || review.calls != 1 {
		t.Fatal(err, review.calls)
	}
}
