package service

import (
	"context"
	"errors"
	"github.com/quenching/training-service/internal/model"
	"testing"
)

type projectionScoreStore struct {
	*fakeFinishStore
	pending bool
	waited  int
}

func (s *projectionScoreStore) ListUnscoredCompleted(context.Context) ([]int64, error) {
	return []int64{1}, nil
}
func (s *projectionScoreStore) WaitForOperations(context.Context, int64) error {
	s.waited++
	if s.pending {
		return errors.New("projection pending")
	}
	s.actions = []model.OperationAction{{Type: "restart_service"}}
	return nil
}
func TestFinalScoreWaitsForProjectionAndRemainsRetryable(t *testing.T) {
	store := &projectionScoreStore{fakeFinishStore: &fakeFinishStore{snapshot: &SessionFinishSnapshot{SessionID: 1, Status: "completed", Difficulty: "easy", TotalDefectCount: 1, FixedDefectCount: 1}}, pending: true}
	svc := NewScoreService(store, &fakeFinishEvaluator{}, nil)
	if _, e := svc.FinishSessionScore(context.Background(), 1, FinishOutcomeCompleted); e == nil || len(store.techUpdates) != 0 {
		t.Fatalf("scored incomplete projection: %v %+v", e, store.techUpdates)
	}
	store.pending = false
	e := svc.RetryPendingScores(context.Background())
	if e != nil || store.waited != 2 || len(store.techUpdates) != 1 || store.techUpdates[0].techScore >= 100 {
		t.Fatalf("late action ignored on retry: %+v %v", store.techUpdates, e)
	}
}
