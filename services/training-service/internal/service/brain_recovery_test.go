package service

import (
	"context"
	"github.com/quenching/training-service/internal/model"
	"strings"
	"testing"
)

type acceptedLocalRecoveryStore struct {
	BrainDispatchStore
	result model.BrainToolResult
}

func (s *acceptedLocalRecoveryStore) PendingCommands(context.Context, int64) ([]model.BrainCommand, error) {
	return []model.BrainCommand{{CommandID: "local1", SessionID: 7, Action: "finish_training", Domain: "training", Status: "accepted"}}, nil
}
func (s *acceptedLocalRecoveryStore) ApplyResult(_ context.Context, _ model.BrainLease, _ model.BrainCommand, r model.BrainToolResult) error {
	s.result = r
	return nil
}
func TestAcceptedLocalMutationRecoveryDoesNotStallForever(t *testing.T) {
	s := &acceptedLocalRecoveryStore{}
	d := NewBrainDispatcher(s, nil, nil)
	pending, err := d.Recover(context.Background(), model.BrainLease{SessionID: 7})
	if err != nil || !pending || s.result.Status != "failed" || !strings.Contains(s.result.ResultJSON, "unconfirmed") {
		t.Fatalf("pending=%t result=%+v err=%v", pending, s.result, err)
	}
}
