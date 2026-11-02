package service

import (
	"encoding/json"
	rc "github.com/quenching/proto/reviewcontract"
	"testing"
)

func TestGrowthCompletionPreservesExistingScores(t *testing.T) {
	report := rc.GrowthReport{SchemaVersion: 1, Overview: json.RawMessage(`{}`), Ability: json.RawMessage(`{"overall_level":"unknown"}`), Claims: []rc.Claim{{Section: "timeline", Category: "user_statement", Text: "我已修复", SourceRefs: []string{"r:1"}}}}
	sections, err := BuildGrowthSections(1, report)
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 6 {
		t.Fatal("missing legacy chapters")
	}
	if sections[1].Content == "我已修复" {
		t.Fatal("source category not shown")
	}
}
