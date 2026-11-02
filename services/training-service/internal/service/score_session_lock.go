package service

import (
	"context"
	"errors"
)

var ErrSessionScoreBusy = errors.New("session scoring already in progress")

type scoreSessionLocker interface {
	WithScoreSessionLock(context.Context, int64, func(context.Context) error) (bool, error)
}

// All finish entry points share this guard, including the periodic retry worker.
// Eligibility and the existing-score check run inside the database claim.
func (s *ScoreService) runSessionScore(ctx context.Context, sid int64, outcome FinishOutcome, requireResolved bool) (*FinishSessionResult, error) {
	locker, ok := s.store.(scoreSessionLocker)
	if !ok {
		return s.finishSessionScore(ctx, sid, outcome, requireResolved)
	}
	var result *FinishSessionResult
	acquired, err := locker.WithScoreSessionLock(ctx, sid, func(attempt context.Context) error {
		var err error
		result, err = s.finishSessionScore(attempt, sid, outcome, requireResolved)
		return err
	})
	if err != nil {
		return result, err
	}
	if !acquired {
		return nil, ErrSessionScoreBusy
	}
	return result, nil
}
