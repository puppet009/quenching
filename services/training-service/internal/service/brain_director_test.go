package service

import (
	"context"
	"github.com/quenching/training-service/internal/model"
	"testing"
	"time"
)

type directorTestStore struct {
	BrainStore
	state   BrainSessionState
	commits int
	waiting string
	closed  bool
}

func (s *directorTestStore) State(context.Context, int64) (*BrainSessionState, error) {
	v := s.state
	return &v, nil
}
func (s *directorTestStore) Acquire(context.Context, int64, string, time.Duration) (*model.BrainLease, error) {
	return &model.BrainLease{SessionID: 1, Owner: "test", Version: 1}, nil
}
func (s *directorTestStore) Renew(context.Context, model.BrainLease, time.Duration) error { return nil }
func (s *directorTestStore) Release(context.Context, model.BrainLease) error              { return nil }
func (s *directorTestStore) CommitDecision(_ context.Context, _ model.BrainLease, _ int64, _ model.BrainDecision, _ []model.BrainCommand) error {
	if s.closed {
		return ErrBrainConflict
	}
	s.commits++
	s.state.ObservationVersion++
	return nil
}
func (s *directorTestStore) SetWaiting(_ context.Context, _ model.BrainLease, r string) error {
	s.waiting = r
	return nil
}

type directorTestModel struct {
	calls  int
	action string
	store  *directorTestStore
	close  bool
}

func (m *directorTestModel) DecideBrain(_ context.Context, o model.BrainObservation) (*model.BrainDecision, error) {
	m.calls++
	if m.close {
		m.store.closed = true
	}
	return &model.BrainDecision{DecisionID: "decision", ObservationVersion: o.ObservationVersion, Action: m.action, ParametersJSON: `{}`, ReasonSummary: "waiting"}, nil
}

type directorTestDispatch struct {
	executions int
	recover    bool
}

func (d *directorTestDispatch) Recover(context.Context, model.BrainLease) (bool, error) {
	return d.recover, nil
}
func (d *directorTestDispatch) Execute(_ context.Context, _ model.BrainLease, c model.BrainCommand) (model.BrainToolResult, error) {
	d.executions++
	return model.BrainToolResult{CommandID: c.CommandID, Status: "succeeded"}, nil
}
func directorFixture(action string) (*BrainDirector, *directorTestStore, *directorTestModel, *directorTestDispatch) {
	s := &directorTestStore{state: BrainSessionState{SessionID: 1, Phase: model.BrainCalibration, BaselineVerified: true}}
	m := &directorTestModel{action: action, store: s}
	dispatch := &directorTestDispatch{}
	obs := func(context.Context, int64) (model.BrainObservation, error) {
		return model.BrainObservation{SessionID: 1, Phase: s.state.Phase, ObservationVersion: s.state.ObservationVersion}, nil
	}
	return NewBrainDirector(s, m, dispatch, obs), s, m, dispatch
}
func TestBaselineBeforeFault(t *testing.T) {
	if ValidateDirectorAction(BrainSessionState{}, model.BrainDecision{Action: "inject_fault"}) == nil {
		t.Fatal("fault before verified baseline")
	}
}
func TestEightCallBudget(t *testing.T) {
	d, s, m, _ := directorFixture("observe_session")
	if e := d.Run(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	if m.calls != 8 || s.waiting == "" {
		t.Fatalf("calls %d waiting %q", m.calls, s.waiting)
	}
}
func TestWaitStopsInference(t *testing.T) {
	d, _, m, x := directorFixture("wait_for_event")
	if e := d.Run(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	if m.calls != 1 || x.executions != 0 {
		t.Fatal("wait kept reasoning or dispatched mutation")
	}
}
func TestSerialChallengeSameSession(t *testing.T) {
	n, e := NextBrainChallenge(3, false)
	if e != nil || n != 4 {
		t.Fatal("challenge number not advanced")
	}
	if _, e = NextBrainChallenge(3, true); e == nil {
		t.Fatal("overlapping serial challenges")
	}
}
func TestLateDecisionAfterAbort(t *testing.T) {
	d, _, m, x := directorFixture("run_user_behavior")
	m.close = true
	if e := d.Run(context.Background(), 1); e != ErrBrainConflict {
		t.Fatalf("closed decision accepted: %v", e)
	}
	if x.executions != 0 {
		t.Fatal("executed late decision")
	}
}
func TestRestartUnknownInjection(t *testing.T) {
	d, _, m, x := directorFixture("observe_session")
	x.recover = true
	if e := d.Run(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	if m.calls != 0 || x.executions != 0 {
		t.Fatal("unknown injection replayed or used as proof")
	}
}
