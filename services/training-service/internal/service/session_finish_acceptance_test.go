package service

import (
	"context"
	"testing"
)

type acceptanceScorer struct{ calls int }

func (s *acceptanceScorer) FinishSessionScore(context.Context, int64, FinishOutcome) (*FinishSessionResult, error) {
	s.calls++
	return &FinishSessionResult{Scored: true}, nil
}

func TestFinishAcceptsPersistedSessionWithoutCallingAI(t *testing.T) {
	store := &leaseFakeStore{finishStatus: "running", finishOK: true}
	scorer := &acceptanceScorer{}
	svc := NewSessionService(store, nil, scorer)
	result, err := svc.Finish(context.Background(), 81, 9, FinishSessionRequest{Outcome: "completed"})
	if err != nil || result == nil || result.Status != "completed" || store.finishStatus != "completed" {
		t.Fatalf("finish was not persisted: result=%+v err=%v", result, err)
	}
	if scorer.calls != 0 || result.Scored {
		t.Fatalf("finish waited for scoring instead of accepting background work: calls=%d result=%+v", scorer.calls, result)
	}
}
