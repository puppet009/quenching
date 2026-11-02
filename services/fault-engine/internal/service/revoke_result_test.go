package service

import (
	"context"
	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/repository"
	"testing"
)

type rejectedDestroy struct{}

func (rejectedDestroy) Run(context.Context, model.InjectSpec, []string) (*BladeExecResult, error) {
	return &BladeExecResult{Raw: `{"code":500,"success":false,"error":"denied"}`}, nil
}
func TestRevokeBusinessFailure(t *testing.T) {
	if NewInjector(rejectedDestroy{}).Revoke(context.Background(), model.InjectSpec{}, "abc123") == nil {
		t.Fatal("reported recovery despite blade business failure")
	}
}

func TestUnconfirmedFaultCleanupRemainsPending(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryExperimentRepo()
	runtime := NewRuntimeService(ctx, rejectedDestroy{}, nil, repo, NewSupervisorRegistry(), nil, RuntimeConfig{})
	defer runtime.Close()
	exp := &model.Experiment{SessionID: 1, Status: model.StatusCreated}
	repo.Create(ctx, exp)
	if e := runtime.StopSession(ctx, 1); e == nil {
		t.Fatal("claimed cleanup of experiment with unknown UID")
	}
	active, _ := repo.ListActiveBySession(ctx, 1)
	if len(active) != 1 {
		t.Fatal("discarded unresolved experiment")
	}
}
func TestFailedRevokeRetainsActiveExperiment(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryExperimentRepo()
	runtime := NewRuntimeService(ctx, rejectedDestroy{}, nil, repo, NewSupervisorRegistry(), nil, RuntimeConfig{})
	defer runtime.Close()
	exp := &model.Experiment{SessionID: 1, Status: model.StatusRunning, ExperimentUID: "abc123"}
	repo.Create(ctx, exp)
	if e := runtime.RevokeOne(ctx, 1, "abc123"); e == nil {
		t.Fatal("unexpected recovery")
	}
	active, _ := repo.ListActiveBySession(ctx, 1)
	if len(active) != 1 {
		t.Fatal("failed revoke removed experiment from recovery queue")
	}
}
