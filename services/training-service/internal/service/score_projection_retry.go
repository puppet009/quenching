package service

import (
	"context"
	"time"
)

type unscoredCompletedReader interface {
	ListUnscoredCompleted(context.Context) ([]int64, error)
}

func (s *ScoreService) RetryPendingScores(ctx context.Context) error {
	reader, ok := s.store.(unscoredCompletedReader)
	if !ok {
		return nil
	}
	ids, e := reader.ListUnscoredCompleted(ctx)
	if e != nil {
		return e
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, _ = s.FinishSessionScore(attempt, id, FinishOutcomeCompleted)
		cancel()
	}
	return nil
}
func (s *ScoreService) RunPendingScores(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_ = s.RetryPendingScores(ctx)
		}
	}
}
