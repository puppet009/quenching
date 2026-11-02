package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ClosedLegacySession struct {
	ID, UserID, ProjectID int64
	Status                string
}

type LegacyClosedSessionStore interface {
	ListClosedLegacySessions(context.Context, int64, int) ([]ClosedLegacySession, error)
}

type LegacyReviewReader interface {
	ListReviewExistence(context.Context, []int64) ([]ReviewExistenceItem, error)
}

// LegacyReviewDispatcher recovers terminal project sessions without a browser.
// Terminal training rows are the durable work source. The cursor only bounds each
// scan; it wraps so unavailable services and process restarts cannot lose work.
type LegacyReviewDispatcher struct {
	store   LegacyClosedSessionStore
	reviews LegacyReviewReader
	trigger ReviewTriggerWriter
	sandbox SandboxLifecycle
	seals   *OperationSealService
	scorer  ClosedSessionScorer
	afterID int64
}

func NewLegacyReviewDispatcher(store LegacyClosedSessionStore, reviews LegacyReviewReader, trigger ReviewTriggerWriter, sandbox SandboxLifecycle, seals *OperationSealService) *LegacyReviewDispatcher {
	return &LegacyReviewDispatcher{store: store, reviews: reviews, trigger: trigger, sandbox: sandbox, seals: seals}
}

func (d *LegacyReviewDispatcher) WithSessionScorer(s ClosedSessionScorer) *LegacyReviewDispatcher {
	d.scorer = s
	return d
}

func (d *LegacyReviewDispatcher) Dispatch(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.store == nil || d.reviews == nil || d.trigger == nil || d.sandbox == nil || d.seals == nil {
		return errors.New("legacy session closure is not configured")
	}
	rows, err := d.store.ListClosedLegacySessions(ctx, d.afterID, 32)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		d.afterID = 0
		return nil
	}
	d.afterID = rows[len(rows)-1].ID
	var failures []error
	ids := make([]int64, 0, len(rows))
	completed := make(map[int64]bool, len(rows))
	// Resource completion must continue even during a review-service outage.
	// Repeating it for existing reviews also recovers a lost finish notification.
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		ids = append(ids, row.ID)
		reason := sandboxReasonManual
		if row.Status == "completed" {
			reason = sandboxReasonCompleted
		} else if row.Status == "failed" {
			reason = sandboxReasonError
		}
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := d.sandbox.MarkCompleted(attempt, row.ID, reason)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("complete legacy session %d: %w", row.ID, err))
			continue
		}
		completed[row.ID] = true
	}
	attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
	existing, err := d.reviews.ListReviewExistence(attempt, ids)
	cancel()
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	hasReview := make(map[int64]bool, len(existing))
	for _, item := range existing {
		if item.ReviewID != nil {
			hasReview[item.SessionID] = true
		}
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if hasReview[row.ID] || !completed[row.ID] {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 25*time.Second)
		seal, err := d.seals.Advance(attempt, row.ID, time.Now().UTC())
		if err == nil && seal.State != "pending" {
			if d.scorer != nil {
				if scoreErr := d.scorer.ScoreClosedSession(attempt, row.ID); scoreErr != nil {
					failures = append(failures, fmt.Errorf("score legacy session %d: %w", row.ID, scoreErr))
					if errors.Is(scoreErr, ErrSessionScoreBusy) {
						cancel()
						continue // The next scan freezes the review after scoring finishes.
					}
				}
			}
			// The remote review service atomically persists its job and enforces
			// one review per session, including concurrent/repeated dispatches.
			err = d.trigger.TriggerGeneration(attempt, row.ID, row.UserID, row.ProjectID)
		}
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("review legacy session %d: %w", row.ID, err))
		}
	}
	return errors.Join(failures...)
}

func (d *LegacyReviewDispatcher) Run(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := d.Dispatch(ctx); err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
