package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/quenching/training-service/internal/model"
	"time"
)

type BrainDirectorStore interface {
	BrainStore
	SetWaiting(context.Context, model.BrainLease, string) error
}
type BrainDecider interface {
	DecideBrain(context.Context, model.BrainObservation) (*model.BrainDecision, error)
}
type BrainDecisionDispatcher interface {
	Recover(context.Context, model.BrainLease) (bool, error)
	Execute(context.Context, model.BrainLease, model.BrainCommand) (model.BrainToolResult, error)
}
type BrainObservationBuilder func(context.Context, int64) (model.BrainObservation, error)
type BrainDirector struct {
	store    BrainDirectorStore
	model    BrainDecider
	dispatch BrainDecisionDispatcher
	observe  BrainObservationBuilder
	closer   interface {
		Close(context.Context, int64) error
	}
	owner string
}

func brainUniqueID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func NewBrainDirector(s BrainDirectorStore, m BrainDecider, d BrainDecisionDispatcher, o BrainObservationBuilder) *BrainDirector {
	return &BrainDirector{store: s, model: m, dispatch: d, observe: o, owner: "director-" + brainUniqueID()}
}
func ValidateDirectorAction(s BrainSessionState, d model.BrainDecision) error {
	if s.Phase == model.BrainClosing || s.Phase == model.BrainReviewing || s.Phase == model.BrainCompleted || s.Phase == model.BrainFailed {
		return ErrBrainConflict
	}
	if d.Action == "inject_fault" && (!s.BaselineVerified || s.ActiveChallenge || s.PendingFaultRevoke) {
		return errors.New("verified baseline and no active challenge required")
	}
	return nil
}
func NextBrainChallenge(current int, active bool) (int, error) {
	if active || current < 0 {
		return 0, ErrBrainConflict
	}
	return current + 1, nil
}

type brainLeaseContextKey struct{}

func WithBrainLease(ctx context.Context, l model.BrainLease) context.Context {
	return context.WithValue(ctx, brainLeaseContextKey{}, l)
}
func BrainLeaseFromContext(ctx context.Context) (model.BrainLease, bool) {
	l, ok := ctx.Value(brainLeaseContextKey{}).(model.BrainLease)
	return l, ok
}
func (d *BrainDirector) OnEvent(ctx context.Context, e model.BrainEvent) error {
	if err := d.store.Ensure(ctx, e.SessionID); err != nil {
		return err
	}
	fresh, err := d.store.AppendEvent(ctx, e)
	if err != nil || !fresh {
		return err
	}
	return d.Run(ctx, e.SessionID)
}
func (d *BrainDirector) Run(parent context.Context, sid int64) error {
	lease, err := d.store.Acquire(parent, sid, d.owner, 30*time.Second)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	ctx = WithBrainLease(ctx, *lease)
	defer func() {
		release, done := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
		defer done()
		_ = d.store.Release(release, *lease)
	}()
	heartbeatDone := make(chan struct{})
	defer func() { cancel(); <-heartbeatDone }()
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if d.store.Renew(ctx, *lease, 30*time.Second) != nil {
					cancel()
					return
				}
			}
		}
	}()
	waiting, err := d.dispatch.Recover(ctx, *lease)
	if err != nil {
		return err
	}
	if waiting {
		return d.store.SetWaiting(ctx, *lease, "waiting for durable command status; no repeat execution")
	}
	for calls := 0; calls < 8; calls++ {
		obs, err := d.observe(ctx, sid)
		if err != nil {
			return err
		}
		decision, err := d.model.DecideBrain(ctx, obs)
		if err != nil {
			_ = d.store.SetWaiting(WithBrainObservationVersion(ctx, obs.ObservationVersion), *lease, "model unavailable; waiting for a new event")
			return err
		}
		if decision == nil {
			return errors.New("empty director decision")
		}
		state, err := d.store.State(ctx, sid)
		if err != nil {
			return err
		}
		if err = ValidateDirectorAction(*state, *decision); err != nil {
			_ = d.store.SetWaiting(WithBrainObservationVersion(ctx, obs.ObservationVersion), *lease, err.Error())
			return err
		}
		decision.DecisionID = fmt.Sprintf("brain:%d:%s", sid, brainUniqueID())
		var intents []model.BrainCommand
		if decision.Action != "wait_for_event" {
			domain := "training"
			switch decision.Action {
			case "run_user_behavior", "verify_business":
				domain = "sandbox"
			case "inject_fault", "revoke_fault":
				domain = "fault"
			}
			intents = []model.BrainCommand{{CommandID: "cmd:" + decision.DecisionID, SessionID: sid, DecisionID: decision.DecisionID, ObservationVersion: obs.ObservationVersion, Domain: domain, Action: decision.Action, ParametersJSON: decision.ParametersJSON, Status: "pending"}}
		}
		if err = d.store.CommitDecision(ctx, *lease, obs.ObservationVersion, *decision, intents); err != nil {
			return err
		}
		if decision.Action == "wait_for_event" {
			return d.store.SetWaiting(ctx, *lease, decision.ReasonSummary)
		}
		result, err := d.dispatch.Execute(ctx, *lease, intents[0])
		if err != nil {
			return err
		}
		if decision.Action == "finish_training" && result.Status == "succeeded" {
			return nil
		}
		if result.Status != "succeeded" && result.Status != "failed" {
			return d.store.SetWaiting(ctx, *lease, "waiting for command result: "+result.Status)
		}
	}
	return d.store.SetWaiting(ctx, *lease, "inference budget exhausted after 8 calls; waiting for a new event")
}

type brainObservationContextKey struct{}

func WithBrainObservationVersion(ctx context.Context, v int64) context.Context {
	return context.WithValue(ctx, brainObservationContextKey{}, v)
}
func BrainObservationVersionFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(brainObservationContextKey{}).(int64)
	return v, ok
}

func (d *BrainDirector) SetCloser(c interface {
	Close(context.Context, int64) error
}) {
	d.closer = c
}
func (d *BrainDirector) Close(ctx context.Context, sid int64) error {
	if d.closer == nil {
		return errors.New("brain cleanup not configured")
	}
	return d.closer.Close(ctx, sid)
}
