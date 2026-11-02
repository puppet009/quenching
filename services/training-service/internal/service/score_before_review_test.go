package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestScoreClosedSessionPreservesResolvedCompletionGate(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		resolved     bool
		want         int
	}{
		{"completed repair", "completed", true, 1},
		{"completed without repair", "completed", false, 0},
		{"aborted repair", "aborted", true, 0},
		{"failed repair", "failed", true, 0},
		{"active repair", "running", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{SessionID: 7, UserID: 9, Status: tc.status, FaultResolved: tc.resolved}}
			if err := NewScoreService(store, &fakeFinishEvaluator{}, nil).ScoreClosedSession(context.Background(), 7); err != nil || len(store.techUpdates) != tc.want {
				t.Fatalf("ended session scoring changed eligibility: err=%v updates=%v", err, store.techUpdates)
			}
		})
	}
}

type orderedBackgroundScore struct {
	steps *[]string
	err   error
}

func (s orderedBackgroundScore) ScoreClosedSession(context.Context, int64) error {
	*s.steps = append(*s.steps, "score")
	return s.err
}

type orderedBackgroundReview struct{ steps *[]string }

func (r orderedBackgroundReview) TriggerGeneration(context.Context, int64, int64, int64) error {
	*r.steps = append(*r.steps, "review")
	return nil
}

func TestBackgroundClosuresAttemptScoringBeforeFreezingReview(t *testing.T) {
	for _, failedScore := range []bool{false, true} {
		for _, kind := range []string{"brain", "legacy"} {
			var steps []string
			var scoreErr error
			if failedScore {
				scoreErr = errors.New("scoring temporarily unavailable")
			}
			scorer := orderedBackgroundScore{steps: &steps, err: scoreErr}
			review := orderedBackgroundReview{steps: &steps}
			var err error
			if kind == "brain" {
				c := NewBrainCleanupCoordinator(&cleanupTestStore{}, cleanupTestFault{}, cleanupTestSandbox{}, review).WithSessionScorer(scorer)
				err = c.Close(context.Background(), 7)
			} else {
				store := &legacyClosedStore{rows: []ClosedLegacySession{{ID: 7}}}
				c := NewLegacyReviewDispatcher(store, &legacyReviewClient{}, review, &legacyCompletedSandbox{}, legacyReadySeals()).WithSessionScorer(scorer)
				err = c.Dispatch(context.Background())
			}
			if !reflect.DeepEqual(steps, []string{"score", "review"}) || failedScore != (err != nil) {
				t.Fatalf("%s closure failed best-effort score ordering: steps=%v err=%v", kind, steps, err)
			}
		}
	}
}

func TestBackgroundClosuresRetryWhileAnotherScorerOwnsSession(t *testing.T) {
	for _, kind := range []string{"brain", "legacy"} {
		var steps []string
		scorer := orderedBackgroundScore{steps: &steps, err: ErrSessionScoreBusy}
		review := orderedBackgroundReview{steps: &steps}
		var err error
		if kind == "brain" {
			c := NewBrainCleanupCoordinator(&cleanupTestStore{}, cleanupTestFault{}, cleanupTestSandbox{}, review).WithSessionScorer(scorer)
			err = c.Close(context.Background(), 7)
		} else {
			store := &legacyClosedStore{rows: []ClosedLegacySession{{ID: 7}}}
			c := NewLegacyReviewDispatcher(store, &legacyReviewClient{}, review, &legacyCompletedSandbox{}, legacyReadySeals()).WithSessionScorer(scorer)
			err = c.Dispatch(context.Background())
		}
		if !errors.Is(err, ErrSessionScoreBusy) || !reflect.DeepEqual(steps, []string{"score"}) {
			t.Fatalf("%s review froze before the concurrent scorer finished: steps=%v err=%v", kind, steps, err)
		}
	}
}
