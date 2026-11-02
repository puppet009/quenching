package service

import (
	"encoding/json"
	oe "github.com/quenching/proto/operationevidence"
	rc "github.com/quenching/proto/reviewcontract"
	"testing"
	"time"
)

func TestOperationMemoryAndSnapshotTrust(t *testing.T) {
	e := oe.Evidence{EventID: "x:3", OperationID: "x", Actor: "assistant", Kind: "file", Status: "unknown", Coverage: oe.Coverage{Execution: "unknown", ReasonCodes: []string{"result_unconfirmed"}}}
	src := OperationReviewSource(e)
	snap, rows, err := BuildReviewSnapshot(rc.Eligibility{UserID: 1, SessionID: 2, ProjectID: 3, SessionStatus: "completed"}, []rc.Source{src}, json.RawMessage(`{}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Actor != "assistant" || rows[0].Trust != "platform_observation" || !rows[0].Incomplete || rows[0].Successful {
		t.Fatal(rows)
	}
	ApplyOperationCoverage(&snap, []oe.Evidence{e}, []oe.AuditSeal{{Producer: "training", State: "ready"}, {Producer: "sandbox", State: "ready"}})
	if snap.AuditComplete || len(snap.AuditGaps) == 0 {
		t.Fatal(snap)
	}
}
func TestSnapshotCoverageSurvivesRetry(t *testing.T) {
	s := rc.Snapshot{}
	ApplyOperationCoverage(&s, nil, nil)
	if s.AuditComplete || len(s.AuditGaps) != 1 || s.AuditGaps[0] != "legacy_coverage_unknown" {
		t.Fatal(s)
	}
	SetReviewSnapshotHash(&s, nil)
	hash := s.Hash
	raw, _ := json.Marshal(s)
	var restored rc.Snapshot
	_ = json.Unmarshal(raw, &restored)
	SetReviewSnapshotHash(&restored, nil)
	if restored.Hash != hash {
		t.Fatal("coverage not immutable")
	}
}

func TestIntermediateOperationDoesNotInventFinalGap(t *testing.T) {
	e := oe.Evidence{OperationID: "x", EventID: "x:1", Sequence: 1, Actor: "student", Kind: "command", Status: "pending", Coverage: oe.EmptyCoverage()}
	pending := OperationReviewSource(e)
	e.Status = "succeeded"
	e.Sequence = 3
	e.Coverage.Execution = "complete"
	e.Coverage.Command = "complete"
	e.Coverage.Output = "complete"
	s := rc.Snapshot{CleanupStatus: "succeeded"}
	ApplyOperationCoverage(&s, []oe.Evidence{e}, []oe.AuditSeal{{Producer: "training", State: "ready"}, {Producer: "sandbox", State: "ready"}})
	ApplyPublicSourceGaps(&s, []rc.Source{pending, OperationReviewSource(e)})
	if !s.AuditComplete {
		t.Fatal(s.AuditGaps)
	}
}

func TestCleanupFailureCannotPromoteOverallAudit(t *testing.T) {
	snap := rc.Snapshot{CleanupStatus: "failed"}
	ApplyOperationCoverage(&snap, nil, []oe.AuditSeal{{Producer: "training", State: "ready"}, {Producer: "sandbox", State: "ready"}})
	ApplyPublicSourceGaps(&snap, nil)
	if snap.AuditComplete {
		t.Fatal("cleanup failure lost")
	}
	var collection map[string]any
	_ = json.Unmarshal(snap.AuditCoverage, &collection)
	if collection["complete"] != true {
		t.Fatal("collection and cleanup must remain separate")
	}
}
