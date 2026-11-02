package service

import (
	"encoding/json"
	oe "github.com/quenching/proto/operationevidence"
	"time"
)

type BusinessProof struct {
	Verified       bool
	Ref            string
	OccurredAt     time.Time
	ExperimentUIDs []string
}
type ChallengeWindow struct {
	StartedAt, ExpiresAt time.Time
	ExperimentUIDs       []string
}
type RepairDecision struct {
	Status, Reason string
	EvidenceRefs   []string
	Actor          string
}

func EvaluateRepair(es []oe.Evidence, before, after BusinessProof, w ChallengeWindow) RepairDecision {
	d := RepairDecision{Status: "repair_unconfirmed", Reason: "insufficient_attributable_repair_evidence", EvidenceRefs: []string{}}
	if !after.Verified || after.Ref == "" {
		return d
	}
	d.EvidenceRefs = append(d.EvidenceRefs, after.Ref)
	if before.Ref == "" || before.Verified || before.OccurredAt.Before(w.StartedAt) || !after.OccurredAt.Before(w.ExpiresAt) {
		d.Status = "recovery_observed"
		d.Reason = "missing_failure_proof_or_fault_expired"
		return d
	}
	d.EvidenceRefs = append(d.EvidenceRefs, before.Ref)
	actors := map[string]bool{}
	proved := []oe.Evidence{}
	for _, e := range es {
		if e.StartedAt.Before(before.OccurredAt) || e.StartedAt.After(after.OccurredAt) {
			continue
		}
		modifies := e.Kind == "file" || e.Kind == "command" || (e.Kind == "tool" && e.Tool == "write_file" && e.Status != "cancelled")
		if !modifies {
			continue
		}
		if e.Actor != "student" && e.Actor != "assistant" && e.Actor != "platform" {
			continue
		}
		if e.Status == "unknown" || e.Status == "pending" || e.Status == "running" || e.Coverage.Execution != "complete" {
			d.Reason = "competing_or_unconfirmed_operation"
			return d
		}
		if e.Kind != "file" {
			// Shell intent does not prove absence of side effects; local write tools
			// must count even before the sandbox file outbox arrives.
			actors[e.Actor] = true
			continue
		}
		if e.Coverage.Before != "complete" || e.Coverage.After != "complete" {
			d.Reason = "competing_or_unconfirmed_operation"
			return d
		}
		var facts struct {
			Changed bool `json:"changed"`
			Read    bool `json:"after_read_success"`
		}
		if json.Unmarshal(e.Result, &facts) != nil {
			d.Reason = "unconfirmed_file_change"
			return d
		}
		if !facts.Changed && e.Status == "succeeded" {
			continue
		}
		actors[e.Actor] = true
		if e.Status == "succeeded" && e.FinishedAt != nil && !e.FinishedAt.After(after.OccurredAt) && e.Coverage.Execution == "complete" && e.Coverage.Before == "complete" && e.Coverage.After == "complete" && facts.Changed && facts.Read {
			proved = append(proved, e)
		}
	}
	if len(actors) > 1 || actors["platform"] {
		d.Reason = "competing_repair_actors"
		return d
	}
	if len(proved) == 0 {
		return d
	}
	d.Actor = proved[len(proved)-1].Actor
	d.Status = "verified_repair"
	if d.Actor == "assistant" {
		d.Status = "assisted_repair"
	}
	d.Reason = "platform_change_and_failed_then_recovered_business"
	for _, e := range proved {
		d.EvidenceRefs = append(d.EvidenceRefs, "operation:"+e.EventID)
	}
	return d
}
