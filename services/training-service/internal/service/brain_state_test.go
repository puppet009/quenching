package service

import (
	"github.com/quenching/training-service/internal/model"
	"testing"
	"time"
)

func TestStaleVersion(t *testing.T) {
	lease := model.BrainLease{SessionID: 1, Owner: "worker", Version: 3, ExpiresAt: time.Now().Add(time.Minute)}
	decision := model.BrainDecision{DecisionID: "d1", ObservationVersion: 2, Action: "wait_for_event", ParametersJSON: "{}"}
	if ValidateBrainCommit(lease, 3, decision, nil) == nil {
		t.Fatal("stale version accepted")
	}
}
func TestLeaseCompetingWriters(t *testing.T) {
	lease := model.BrainLease{SessionID: 1, Version: 3, ExpiresAt: time.Now().Add(time.Minute)}
	d := model.BrainDecision{DecisionID: "d1", ObservationVersion: 2, Action: "wait_for_event", ParametersJSON: "{}"}
	if ValidateBrainCommit(lease, 2, d, nil) == nil {
		t.Fatal("missing lease owner accepted")
	}
}
func TestDuplicateEvent(t *testing.T) {
	event := model.BrainEvent{SessionID: 1, Source: "runner", EventID: "cmd1", Kind: "business_result"}
	if err := ValidateBrainEvent(event); err != nil {
		t.Fatal(err)
	}
	event.Source = ""
	if ValidateBrainEvent(event) == nil {
		t.Fatal("event without dedup source accepted")
	}
}
func TestRestartPendingCommand(t *testing.T) {
	for _, status := range []string{"accepted", "running", "unknown"} {
		cmd := model.BrainCommand{Status: status}
		if BrainRecoveryAction(cmd) != "query_status" {
			t.Fatal("unknown command must query before repeat")
		}
	}
	if BrainRecoveryAction(model.BrainCommand{Status: "succeeded"}) != "none" {
		t.Fatal("completed command repeated")
	}
	if BrainRecoveryAction(model.BrainCommand{Status: "pending"}) != "dispatch" {
		t.Fatal("never-dispatched command not recoverable")
	}
}
