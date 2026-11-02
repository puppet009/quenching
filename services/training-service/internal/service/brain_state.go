package service

import (
	"context"
	"errors"
	"github.com/quenching/training-service/internal/model"
	"time"
)

var ErrBrainConflict = errors.New("brain state conflict or closed session")

type BrainSessionState struct {
	PendingFaultRevoke bool             `json:"pending_fault_revoke"`
	SessionID          int64            `json:"session_id"`
	Phase              model.BrainPhase `json:"phase"`
	ObservationVersion int64            `json:"observation_version"`
	BaselineVerified   bool             `json:"baseline_verified"`
	ActiveChallenge    bool             `json:"active_challenge"`
	CurrentChallenge   int              `json:"current_challenge"`
	HintLevel          int              `json:"hint_level"`
	HintBudget         int              `json:"hint_budget"`
	Reason             string           `json:"reason"`
	CleanupStatus      string           `json:"cleanup_status"`
}
type BrainStore interface {
	Ensure(context.Context, int64) error
	State(context.Context, int64) (*BrainSessionState, error)
	Acquire(context.Context, int64, string, time.Duration) (*model.BrainLease, error)
	Renew(context.Context, model.BrainLease, time.Duration) error
	Release(context.Context, model.BrainLease) error
	AppendEvent(context.Context, model.BrainEvent) (bool, error)
	CommitDecision(context.Context, model.BrainLease, int64, model.BrainDecision, []model.BrainCommand) error
	PendingCommands(context.Context, int64) ([]model.BrainCommand, error)
	CompleteCommand(context.Context, string, model.BrainToolResult) error
}

func ValidateBrainEvent(event model.BrainEvent) error {
	if event.SessionID <= 0 || event.EventID == "" || event.Source == "" || event.Kind == "" {
		return errors.New("invalid brain event identity")
	}
	return nil
}
func ValidateBrainCommit(lease model.BrainLease, expected int64, d model.BrainDecision, intents []model.BrainCommand) error {
	if lease.SessionID <= 0 || lease.Owner == "" || lease.Version <= 0 || d.DecisionID == "" || d.ObservationVersion != expected || !brainActions[d.Action] {
		return ErrBrainConflict
	}
	seen := map[string]bool{}
	for _, cmd := range intents {
		if cmd.CommandID == "" || seen[cmd.CommandID] || cmd.SessionID != lease.SessionID || cmd.DecisionID != d.DecisionID || cmd.ObservationVersion != expected || cmd.Action != d.Action {
			return errors.New("invalid action intent")
		}
		seen[cmd.CommandID] = true
	}
	return nil
}
func BrainRecoveryAction(cmd model.BrainCommand) string {
	switch cmd.Status {
	case "pending":
		return "dispatch"
	case "accepted", "running", "unknown":
		return "query_status"
	default:
		return "none"
	}
}
