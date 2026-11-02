package service

import (
	"context"
	"errors"
	"testing"
)

type cleanupTestStore struct {
	BrainCleanupStore
	status    string
	requested bool
	evidence  []string
}

func (s *cleanupTestStore) Freeze(context.Context, int64, string) error { return nil }
func (s *cleanupTestStore) CleanupState(context.Context, int64) (BrainCleanupState, error) {
	return BrainCleanupState{UserID: 1, ReportRequested: s.requested}, nil
}
func (s *cleanupTestStore) RecordCleanup(_ context.Context, _ int64, status, reason string) error {
	s.status = status
	return nil
}
func (s *cleanupTestStore) MarkReportRequested(context.Context, int64) error {
	s.requested = true
	return nil
}

type cleanupTestFault struct{ fail bool }

func (f cleanupTestFault) StopSession(context.Context, int64) error {
	if f.fail {
		return errors.New("agent unavailable")
	}
	return nil
}

type cleanupTestSandbox struct{}

func (cleanupTestSandbox) MarkCompleted(context.Context, int64, string) error { return nil }
func (cleanupTestSandbox) CleanupBrainSession(context.Context, int64) (string, error) {
	return "succeeded", nil
}

type cleanupTestReview struct{ calls int }

func (r *cleanupTestReview) TriggerGeneration(context.Context, int64, int64, int64) error {
	r.calls++
	return nil
}
func TestCleanupFailureSeparate(t *testing.T) {
	s := &cleanupTestStore{}
	review := &cleanupTestReview{}
	c := NewBrainCleanupCoordinator(s, cleanupTestFault{true}, cleanupTestSandbox{}, review)
	if c.Close(context.Background(), 1) == nil || s.status != "failed" || review.calls != 1 {
		t.Fatalf("status=%s report=%d", s.status, review.calls)
	}
}
func TestCleanupKeepsPlatformEvidence(t *testing.T) {
	s := &cleanupTestStore{evidence: []string{"e1"}}
	c := NewBrainCleanupCoordinator(s, cleanupTestFault{}, cleanupTestSandbox{}, &cleanupTestReview{})
	if e := c.Close(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	if len(s.evidence) != 1 || s.status != "succeeded" {
		t.Fatal("deleted audit with sandbox")
	}
}
