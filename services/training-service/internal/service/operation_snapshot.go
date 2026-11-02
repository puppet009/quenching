package service

import (
	"encoding/json"
	oe "github.com/quenching/proto/operationevidence"
	rc "github.com/quenching/proto/reviewcontract"
	"sort"
)

func operationGaps(e oe.Evidence) []string {
	gaps := append([]string{}, e.Coverage.ReasonCodes...)
	fields := []string{e.Coverage.Execution}
	switch e.Kind {
	case "command":
		fields = append(fields, e.Coverage.Command, e.Coverage.Output)
	case "tool":
		fields = append(fields, e.Coverage.Output)
	case "file":
		fields = append(fields, e.Coverage.Before, e.Coverage.After)
	case "terminal":
		gaps = append(gaps, "interactive_untracked")
	}
	for _, v := range fields {
		if v != "complete" {
			gaps = append(gaps, "operation_coverage_incomplete")
			break
		}
	}
	if !oe.Terminal(e.Status) || e.Status == "unknown" {
		gaps = append(gaps, "result_unconfirmed")
	}
	return gaps
}
func OperationReviewSource(e oe.Evidence) rc.Source {
	raw, _ := json.Marshal(oe.Sanitize(e))
	return rc.Source{ID: "operation:" + e.EventID, Kind: "operation_evidence", Actor: e.Actor, Text: string(raw), OccurredAt: e.StartedAt, Incomplete: len(operationGaps(e)) > 0}
}

// Completion describes collection, independently of sandbox cleanup and business recovery.
func ApplyOperationCoverage(s *rc.Snapshot, events []oe.Evidence, seals []oe.AuditSeal) {
	gaps := map[string]bool{}
	producers := map[string]bool{}
	for _, seal := range seals {
		producers[seal.Producer] = true
		for _, g := range seal.Gaps {
			gaps[g] = true
		}
		if seal.State != "ready" {
			gaps["audit_delivery_timeout"] = true
		}
	}
	if !producers["training"] || !producers["sandbox"] {
		gaps["legacy_coverage_unknown"] = true
	}
	// Intermediate pending/running events remain sources but final coverage uses each operation's latest state.
	latest := map[string]oe.Evidence{}
	for _, e := range events {
		if old, ok := latest[e.OperationID]; !ok || e.Sequence > old.Sequence {
			latest[e.OperationID] = e
		}
	}
	for _, e := range latest {
		for _, g := range operationGaps(e) {
			gaps[g] = true
		}
	}
	s.AuditGaps = []string{}
	for g := range gaps {
		s.AuditGaps = append(s.AuditGaps, g)
	}
	sort.Strings(s.AuditGaps)
	s.AuditComplete = s.CleanupStatus == "succeeded" && len(gaps) == 0
	s.AuditCoverage, _ = json.Marshal(map[string]any{"complete": len(gaps) == 0, "producers": seals, "operation_count": len(latest), "scope": "managed_commands_tools_and_platform_files"})
}

func ApplyPublicSourceGaps(s *rc.Snapshot, sources []rc.Source) {
	gaps := map[string]bool{}
	for _, g := range s.AuditGaps {
		gaps[g] = true
	}
	for _, src := range sources {
		if src.Kind == "terminal" {
			gaps["interactive_untracked"] = true
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal([]byte(src.Text), &raw) != nil {
			continue
		}
		var reason string
		if json.Unmarshal(raw["reason"], &reason) == nil && (reason == "interactive_untracked" || reason == "unsafe_record_boundary" || reason == "output_limit_exceeded") {
			gaps[reason] = true
		}
		if src.Incomplete && src.Kind != "operation_evidence" {
			gaps["operation_coverage_incomplete"] = true
		}
	}
	s.AuditGaps = []string{}
	for g := range gaps {
		s.AuditGaps = append(s.AuditGaps, g)
	}
	sort.Strings(s.AuditGaps)
	s.AuditComplete = s.CleanupStatus == "succeeded" && len(gaps) == 0
	var coverage map[string]any
	_ = json.Unmarshal(s.AuditCoverage, &coverage)
	if coverage == nil {
		coverage = map[string]any{}
	}
	coverage["complete"] = len(gaps) == 0
	s.AuditCoverage, _ = json.Marshal(coverage)
}
