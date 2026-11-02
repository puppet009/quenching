package service

import (
	"context"
	oe "github.com/quenching/proto/operationevidence"
	oetest "github.com/quenching/proto/operationevidence/testutil"
	"testing"
)

func TestForeignOperationSourceDenied(t *testing.T) {
	s := &OperationEvidenceService{Store: oetest.NewMemoryStore(), Owned: func(_ context.Context, uid, sid int64) error {
		if uid != 7 || sid != 1 {
			return oe.ErrNotFound
		}
		return nil
	}}
	if _, err := s.ListOwned(context.Background(), 8, 1, 0, 20); err == nil {
		t.Fatal("foreign operation visible")
	}
	if _, err := s.ReadOwned(context.Background(), 8, 1, "optext:x:stdout", 0, 8192); err == nil {
		t.Fatal("foreign text visible")
	}
}
