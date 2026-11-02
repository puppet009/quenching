package tooljournal

import (
	"context"
	"testing"
)

func TestProducerJournalAndAnalysisProjectionAreDistinct(t *testing.T) {
	query := "SELECT evidence FROM training_operation_events JOIN training_operation_intents USING(operation_id)"
	if got := operationSQL(query, true); got != "SELECT evidence FROM training_tool_events JOIN training_tool_intents USING(operation_id)" {
		t.Fatal(got)
	}
	if got := operationSQL(query, false); got != query {
		t.Fatal(got)
	}
}
func TestProjectionCannotBeginToolExecution(t *testing.T) {
	defer func() {
		if recover() != nil {
			t.Fatal("projection attempted DB mutation before refusing")
		}
	}()
	store := &PGStore{}
	_, err := store.Begin(context.Background(), 1, "x", "key", "hash", Evidence{SchemaVersion: 1, SessionID: 1, OperationID: "x", EventID: "x:1", Sequence: 1, Actor: "assistant", Kind: "tool", Status: "pending"})
	if err == nil {
		t.Fatal("projection accepted producer mutation")
	}
}
