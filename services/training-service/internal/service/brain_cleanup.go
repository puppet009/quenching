package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type BrainCleanupState struct {
	UserID, ProjectID int64
	ReportRequested   bool
}
type BrainCleanupStore interface {
	Freeze(context.Context, int64, string) error
	CleanupState(context.Context, int64) (BrainCleanupState, error)
	RecordCleanup(context.Context, int64, string, string) error
	MarkReportRequested(context.Context, int64) error
}
type BrainCleanupFault interface {
	StopSession(context.Context, int64) error
}
type BrainCleanupSandbox interface {
	MarkCompleted(context.Context, int64, string) error
	CleanupBrainSession(context.Context, int64) (string, error)
}
type BrainCleanupReview interface {
	TriggerGeneration(context.Context, int64, int64, int64) error
}
type BrainCleanupCoordinator struct {
	auditSeal *OperationSealService
	store     BrainCleanupStore
	fault     BrainCleanupFault
	sandbox   BrainCleanupSandbox
	review    BrainCleanupReview
	scorer    ClosedSessionScorer
}

func NewBrainCleanupCoordinator(s BrainCleanupStore, f BrainCleanupFault, b BrainCleanupSandbox, r BrainCleanupReview) *BrainCleanupCoordinator {
	return &BrainCleanupCoordinator{store: s, fault: f, sandbox: b, review: r}
}

// Close preserves evidence even when resource deletion fails. Remote operations must be idempotent.
func (c *BrainCleanupCoordinator) Close(ctx context.Context, sid int64) error {
	if err := c.store.Freeze(ctx, sid, "training closed"); err != nil {
		return err
	}
	state, err := c.store.CleanupState(ctx, sid)
	if err != nil {
		return err
	}
	if c.auditSeal != nil {
		if c.sandbox != nil {
			// A lost remote freeze must not prevent the durable local seal deadline.
			// Retry it below after pending collection has completed or timed out.
			_ = c.sandbox.MarkCompleted(ctx, sid, "completed")
		}
		seal, e := c.auditSeal.Advance(ctx, sid, time.Now().UTC())
		if e != nil {
			return e
		}
		if seal.State == "pending" {
			return nil
		}
	}
	var failures []error
	frozen := false
	if c.sandbox == nil {
		failures = append(failures, errors.New("sandbox cleanup unavailable"))
	} else if err := c.sandbox.MarkCompleted(ctx, sid, "completed"); err != nil {
		failures = append(failures, err)
	} else {
		frozen = true
	}
	revoked := false
	if c.fault == nil {
		failures = append(failures, errors.New("fault cleanup unavailable"))
	} else if err := c.fault.StopSession(ctx, sid); err != nil {
		failures = append(failures, err)
	} else {
		revoked = true
	}
	status := "pending"
	// Keep the namespace until revocation is proved; deleting the agent first would lose cleanup access.
	if frozen && revoked {
		status, err = c.sandbox.CleanupBrainSession(ctx, sid)
		if err != nil {
			failures = append(failures, err)
		}
		if status != "pending" && status != "succeeded" {
			failures = append(failures, fmt.Errorf("invalid cleanup status %q", status))
		}
	}
	if len(failures) > 0 {
		status = "failed"
	}
	reason := ""
	if joined := errors.Join(failures...); joined != nil {
		reason = joined.Error()
	}
	if err = c.store.RecordCleanup(ctx, sid, status, reason); err != nil {
		return errors.Join(append(failures, err)...)
	}
	// Report generation and cleanup are independent durable outcomes.
	if !state.ReportRequested && status != "pending" {
		if c.scorer != nil {
			if err := c.scorer.ScoreClosedSession(ctx, sid); err != nil {
				failures = append(failures, fmt.Errorf("background scoring: %w", err))
				if errors.Is(err, ErrSessionScoreBusy) {
					return errors.Join(failures...) // Retry before freezing an incomplete score.
				}
			}
		}
		if c.review == nil {
			failures = append(failures, errors.New("review service unavailable"))
		} else if err = c.review.TriggerGeneration(ctx, sid, state.UserID, state.ProjectID); err != nil {
			failures = append(failures, err)
		} else if err = c.store.MarkReportRequested(ctx, sid); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c *BrainCleanupCoordinator) WithOperationSeal(s *OperationSealService) *BrainCleanupCoordinator {
	c.auditSeal = s
	return c
}

func (c *BrainCleanupCoordinator) WithSessionScorer(s ClosedSessionScorer) *BrainCleanupCoordinator {
	c.scorer = s
	return c
}
