package repository

import (
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/training-service/internal/service"
	"testing"
	"time"
)

func TestPGToolRecoveryRespectsLiveReplyLease(t *testing.T) {
	ctx, memory, pool := sessionMemoryDB(t)
	conversation, err := memory.Resolve(ctx, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	store := NewToolJournal(pool)
	e := oe.Evidence{SchemaVersion: 1, OperationID: "orphan", EventID: "orphan:1", Sequence: 1, SessionID: 1, Actor: "assistant", Kind: "tool", Status: "pending", ConversationID: conversation.ConversationID, StartedAt: time.Now(), Coverage: oe.EmptyCoverage()}
	if _, err = store.Begin(ctx, 1, "orphan", "orphan", "hash", e); err != nil {
		t.Fatal(err)
	}
	read := OrphanedToolOperations(pool)
	events, err := read(ctx)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if _, err = memory.Acquire(ctx, 10, 1, "test-reply", time.Minute); err != nil {
		t.Fatal(err)
	}
	events, err = read(ctx)
	if err != nil || len(events) != 0 {
		t.Fatal("live approval incorrectly expired", events, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE session_assistant_identity SET lease_expires=clock_timestamp()-interval '1 second' WHERE session_id=1`); err != nil {
		t.Fatal(err)
	}
	if err = (service.ToolRecovery{Store: store, Orphans: read}).Recover(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Get(ctx, 1, "orphan")
	if err != nil || restored.Status != "unknown" {
		t.Fatal(restored, err)
	}
}
