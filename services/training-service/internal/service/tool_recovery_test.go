package service

import (
	"context"
	oe "github.com/quenching/proto/operationevidence"
	oetest "github.com/quenching/proto/operationevidence/testutil"
	"testing"
	"time"
)

func TestOrphanedToolBecomesUnknownWithoutReplay(t *testing.T) {
	ctx := context.Background()
	store := oetest.NewMemoryStore()
	e := oe.Evidence{SchemaVersion: 1, OperationID: "orphan", EventID: "orphan:1", Sequence: 1, SessionID: 7, Actor: "assistant", Kind: "tool", Status: "pending", Approval: "requested", ApprovalID: "lost-broker-token", StartedAt: time.Now(), Coverage: oe.EmptyCoverage()}
	if _, err := store.Begin(ctx, 7, e.OperationID, e.OperationID, "hash", e); err != nil {
		t.Fatal(err)
	}
	recovery := ToolRecovery{Store: store, Orphans: func(context.Context) ([]oe.Evidence, error) { return []oe.Evidence{e}, nil }}
	if err := recovery.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	v, _ := store.Get(ctx, 7, e.OperationID)
	if v.Status != "unknown" || v.ExitCode != nil || v.Approval != "expired" {
		t.Fatal(v)
	}
	// Reconciliation of the old intent is idempotent and never begins a fresh operation.
	if err := recovery.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	page, _ := store.List(ctx, 7, 0, 20)
	if len(page.Items) != 1 {
		t.Fatal("replayed", page)
	}
}
