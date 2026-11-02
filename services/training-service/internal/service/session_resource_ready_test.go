package service

import (
	"context"
	"testing"
)

type resourceStartStore struct {
	SessionStore
	starts int
}

func (s *resourceStartStore) GetSessionStatus(context.Context, int64, int64) (string, bool, error) {
	return "pending", true, nil
}
func (s *resourceStartStore) StartSession(context.Context, int64, int64) (bool, error) {
	s.starts++
	return true, nil
}
func TestQueuedSessionCannotStartTimer(t *testing.T) {
	store := &resourceStartStore{}
	svc := NewSessionService(store, nil, nil)
	ready := false
	svc.SetSandboxReadiness(func(context.Context, int64, int64) (bool, error) { return ready, nil })
	if _, e := svc.Start(context.Background(), 9, 2); e == nil || store.starts != 0 {
		t.Fatal("queued session started", e, store.starts)
	}
	ready = true
	if _, e := svc.Start(context.Background(), 9, 2); e != nil || store.starts != 1 {
		t.Fatal(e, store.starts)
	}
}
