package service

import (
	oe "github.com/quenching/proto/operationevidence"
	"testing"
	"time"
)

func repairFixture() ([]oe.Evidence, BusinessProof, BusinessProof, ChallengeWindow) {
	now := time.Now()
	start := now.Add(-time.Minute)
	finish := now.Add(-10 * time.Second)
	e := oe.Evidence{OperationID: "repair", Actor: "student", Kind: "file", Status: "succeeded", StartedAt: now.Add(-20 * time.Second), FinishedAt: &finish, Coverage: oe.Coverage{Before: "complete", After: "complete", Execution: "complete"}, Result: []byte(`{"changed":true,"after_read_success":true}`)}
	return []oe.Evidence{e}, BusinessProof{Ref: "failed", OccurredAt: now.Add(-40 * time.Second)}, BusinessProof{Verified: true, Ref: "recovered", OccurredAt: now}, ChallengeWindow{StartedAt: start, ExpiresAt: now.Add(time.Minute)}
}
func TestRepairNeedsFailedThenRecoveredBusiness(t *testing.T) {
	es, b, a, w := repairFixture()
	if EvaluateRepair(es, b, a, w).Status != "verified_repair" {
		t.Fatal("proved file repair lost")
	}
	b.Ref = ""
	if EvaluateRepair(es, b, a, w).Status == "verified_repair" {
		t.Fatal("missing failure proof")
	}
}
func TestRepairDiagnosisCannotPass(t *testing.T) {
	es, b, a, w := repairFixture()
	es[0].Kind = "command"
	es[0].Command = "ps aux"
	es[0].Intent = "repair"
	zero := 0
	es[0].ExitCode = &zero
	if EvaluateRepair(es, b, a, w).Status == "verified_repair" {
		t.Fatal("diagnosis promoted")
	}
}
func TestRepairUnknownOrExpiryCannotPass(t *testing.T) {
	es, b, a, w := repairFixture()
	es[0].Status = "unknown"
	if EvaluateRepair(es, b, a, w).Status == "verified_repair" {
		t.Fatal("unknown")
	}
	es, b, a, w = repairFixture()
	w.ExpiresAt = a.OccurredAt.Add(-time.Second)
	if EvaluateRepair(es, b, a, w).Status == "verified_repair" {
		t.Fatal("expiry")
	}
}
func TestAssistantRepairNotStudentPromotion(t *testing.T) {
	es, b, a, w := repairFixture()
	es[0].Actor = "assistant"
	if EvaluateRepair(es, b, a, w).Status != "assisted_repair" {
		t.Fatal("assistant promoted student")
	}
}
func TestRepairConcurrentActorUnconfirmed(t *testing.T) {
	es, b, a, w := repairFixture()
	other := es[0]
	other.Actor = "assistant"
	other.OperationID = "assistant"
	es = append(es, other)
	if EvaluateRepair(es, b, a, w).Status != "repair_unconfirmed" {
		t.Fatal("ambiguous actor")
	}
}

func TestUnknownAssistantChangeCannotGrantStudentRepair(t *testing.T) {
	es, b, a, w := repairFixture()
	other := es[0]
	other.Actor = "assistant"
	other.OperationID = "assistant-unknown"
	other.Coverage.Before = "unknown"
	other.Result = []byte(`{"changed":false,"after_read_success":true}`)
	if d := EvaluateRepair(append(es, other), b, a, w); d.Status != "repair_unconfirmed" {
		t.Fatal(d)
	}
	other.Kind = "command"
	other.Intent = "repair"
	other.Coverage.Execution = "complete"
	other.Status = "succeeded"
	if d := EvaluateRepair(append(es, other), b, a, w); d.Status != "repair_unconfirmed" {
		t.Error("competing command", d)
	}
	other.Kind = "tool"
	other.Tool = "write_file"
	if d := EvaluateRepair(append(es, other), b, a, w); d.Status != "repair_unconfirmed" {
		t.Error("local tool before remote file delivery", d)
	}
}
