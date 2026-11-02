package service

import (
	"context"
	"time"
)

type ClosedSessionScorer interface {
	ScoreClosedSession(context.Context, int64) error
}

// ScoreClosedSession attempts the previous finish scoring before the immutable
// review snapshot is created. It runs only in backend closure workers.
func (s *ScoreService) ScoreClosedSession(ctx context.Context, sid int64) error {
	attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// The finish outcome alone does not prove a repair. Preserve the stored
	// completed+resolved gate inside the lock, including when tech was already
	// written by a concurrent scorer whose collaborative writes are in flight.
	_, err := s.runSessionScore(attempt, sid, FinishOutcomeCompleted, true)
	return err
}
