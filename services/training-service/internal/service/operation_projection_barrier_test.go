package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	oe "github.com/quenching/proto/operationevidence"
	rc "github.com/quenching/proto/reviewcontract"
)

func closeProjectionFixture(kind, outcome string, seals *OperationSealService, reports *cleanupTestReview) (func() error, *fakeFinishStore) {
	scores := &fakeFinishStore{snapshot: &SessionFinishSnapshot{SessionID: 7, UserID: 9, Status: outcome, FaultResolved: false}}
	scorer := NewScoreService(scores, &fakeFinishEvaluator{}, nil)
	if kind == "brain" {
		coordinator := NewBrainCleanupCoordinator(&cleanupTestStore{}, cleanupTestFault{}, cleanupTestSandbox{}, reports).WithOperationSeal(seals).WithSessionScorer(scorer)
		return func() error { return coordinator.Close(context.Background(), 7) }, scores
	}
	rows := &legacyClosedStore{rows: []ClosedLegacySession{{ID: 7, UserID: 9, ProjectID: 2, Status: outcome}}}
	return func() error {
		// Recreating the worker also proves that the audit deadline belongs to
		// the persisted seal, rather than the lifetime of a browser or worker.
		return NewLegacyReviewDispatcher(rows, &legacyReviewClient{}, reports, &legacyCompletedSandbox{}, seals).WithSessionScorer(scorer).Dispatch(context.Background())
	}, scores
}

func TestAllReportOutcomesWaitForCapturedLegacyOperationProjection(t *testing.T) {
	for _, kind := range []string{"brain", "legacy"} {
		for _, outcome := range []string{"aborted", "failed", "completed"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				seals := legacyReadySeals()
				deadline := seals.Local.(*sealFake).seal.Deadline
				ready, checks := false, 0
				seals.ProjectionReady = func(ctx context.Context, sid int64) error {
					checks++
					if sid != 7 {
						t.Fatalf("checkpoint lost session scope: %d", sid)
					}
					if !ready {
						return errors.New("captured operation is not projected yet")
					}
					return nil
				}
				reports := &cleanupTestReview{}
				closeSession, scores := closeProjectionFixture(kind, outcome, seals, reports)
				if err := closeSession(); err != nil || reports.calls != 0 {
					t.Fatalf("report froze before captured operation was projected: err=%v reports=%d", err, reports.calls)
				}
				saved := seals.Local.(*sealFake).saved
				if checks != 1 || saved.State != "pending" || saved.Pending == 0 || !saved.Deadline.Equal(deadline) {
					t.Fatalf("projection did not preserve the pending durable seal: checks=%d saved=%+v", checks, saved)
				}
				ready = true
				if err := closeSession(); err != nil || reports.calls != 1 || len(scores.techUpdates) != 0 {
					t.Fatalf("unscored report did not resume after projection: err=%v reports=%d scores=%v", err, reports.calls, scores.techUpdates)
				}
				if checks != 2 || seals.Local.(*sealFake).saved.State != "ready" {
					t.Fatalf("ready checkpoint was not rechecked: checks=%d saved=%+v", checks, seals.Local.(*sealFake).saved)
				}
			})
		}
	}
}

func TestProjectionOutageTimesOutAtPersistedDeadlineAndReportsCoverageGap(t *testing.T) {
	for _, kind := range []string{"brain", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			seals := legacyReadySeals()
			deadline := time.Now().Add(-time.Second)
			seals.Local.(*sealFake).seal.Deadline = deadline
			seals.ProjectionReady = func(context.Context, int64) error { return errors.New("checkpoint unavailable") }
			reports := &cleanupTestReview{}
			closeSession, _ := closeProjectionFixture(kind, "aborted", seals, reports)
			if err := closeSession(); err != nil || reports.calls != 1 {
				t.Fatalf("checkpoint outage blocked a report after its audit deadline: err=%v reports=%d", err, reports.calls)
			}
			saved := seals.Local.(*sealFake).saved
			if saved.State != "timed_out" || !saved.Deadline.Equal(deadline) || !slices.Contains(saved.Gaps, "legacy_projection_timeout") {
				t.Fatalf("checkpoint gap was absent or deadline was extended: %+v", saved)
			}
			snapshot := rc.Snapshot{CleanupStatus: "succeeded"}
			ApplyOperationCoverage(&snapshot, nil, []oe.AuditSeal{seals.Local.(*sealFake).seal, saved})
			if snapshot.AuditComplete || !slices.Contains(snapshot.AuditGaps, "legacy_projection_timeout") {
				t.Fatalf("projection timeout disappeared from frozen coverage: %+v", snapshot)
			}
		})
	}
}

func TestProjectionCheckpointUsesBoundedContext(t *testing.T) {
	seals := legacyReadySeals()
	called := false
	seals.ProjectionReady = func(ctx context.Context, _ int64) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Fatal("checkpoint could wait indefinitely inside the closure worker")
		}
		return nil
	}
	if _, err := seals.Advance(context.Background(), 7, time.Now()); err != nil || !called {
		t.Fatalf("checkpoint was not called: called=%v err=%v", called, err)
	}
}
