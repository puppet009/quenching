package service

import (
	"encoding/json"
	rc "github.com/quenching/proto/reviewcontract"
	"testing"
)

func TestAuditGapCannotBePromoted(t *testing.T) {
	r := rc.GrowthReport{SchemaVersion: 1, Overview: json.RawMessage(`{"audit_complete":true}`), Ability: json.RawMessage(`{"overall_level":"unknown"}`)}
	if ValidateGrowthReport(r, rc.Snapshot{AuditComplete: false, AuditGaps: []string{"result_unconfirmed"}}, nil) == nil {
		t.Fatal("model promoted audit")
	}
	var ability map[string]any
	_ = json.Unmarshal(FrozenGrowthAbilityWithAudit(nil, rc.Snapshot{AuditGaps: []string{"result_unconfirmed"}}), &ability)
	if ability["overall_level"] != "unknown" {
		t.Fatal(ability)
	}
}
