package service

import (
	"encoding/json"
	rc "github.com/quenching/proto/reviewcontract"
	"testing"
)

func TestGrowthClaimsRejectSelfReportObservation(t *testing.T) {
	sources := []rc.Source{{ID: "r:1", Trust: "user_statement", Actor: "student_report"}}
	claims := []rc.Claim{{Section: "root_cause", Category: "observed", Text: "修复成功", SourceRefs: []string{"r:1"}}}
	if ValidateGrowthClaims(claims, sources, nil) == nil {
		t.Fatal("self-report became observed fact")
	}
}
func TestGrowthReportKeepsUnknownAbility(t *testing.T) {
	report := rc.GrowthReport{SchemaVersion: 1, Overview: json.RawMessage(`{}`), Ability: json.RawMessage(`{"overall_level":"senior"}`)}
	if ValidateGrowthReport(report, rc.Snapshot{}, nil) == nil {
		t.Fatal("model assigned unsupported overall level")
	}
}
