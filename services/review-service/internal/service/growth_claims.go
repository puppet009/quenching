package service

import (
	"encoding/json"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"strings"
	"unicode/utf8"
)

func ValidateGrowthClaims(claims []rc.Claim, sources []rc.Source, allowedURLs []string) error {
	if len(claims) > 128 {
		return fmt.Errorf("too many growth claims")
	}
	owned := map[string]rc.Source{}
	urls := map[string]bool{}
	for _, s := range sources {
		owned[s.ID] = s
	}
	for _, u := range allowedURLs {
		urls[u] = true
	}
	sections := map[string]bool{"root_cause": true, "timeline": true, "path_comparison": true, "highlights_pitfalls": true, "methodology": true, "knowledge": true}
	for _, c := range claims {
		if !sections[c.Section] || strings.TrimSpace(c.Text) == "" || utf8.RuneCountInString(c.Text) > 2000 || len(c.SourceRefs) < 1 || len(c.SourceRefs) > 8 || len(c.SourceURLs) > 8 {
			return fmt.Errorf("invalid growth claim")
		}
		scopes := map[string]bool{}
		matched := false
		for _, r := range c.SourceRefs {
			s, ok := owned[r]
			if !ok {
				return fmt.Errorf("foreign growth source")
			}
			scopes[s.ChallengeID] = true
			switch c.Category {
			case "observed":
				if s.Actor == "student_report" || (s.Trust != "platform_observation" && s.Trust != "fault_reference" && s.Trust != "challenge" && s.Trust != "assessment") {
					return fmt.Errorf("observation requires authority")
				}
			case "user_statement", "assistant_suggestion":
				matched = matched || s.Trust == c.Category
			case "hypothesis", "industry_advice":
			default:
				return fmt.Errorf("invalid claim category")
			}
		}
		if (c.Category == "user_statement" || c.Category == "assistant_suggestion") && !matched {
			return fmt.Errorf("source category mismatch")
		}
		if c.ChallengeID != "" && !scopes[c.ChallengeID] {
			return fmt.Errorf("unknown challenge scope")
		}
		comparisons := map[string]bool{}
		for _, id := range c.ComparisonChallengeIDs {
			if id == "" || !scopes[id] {
				return fmt.Errorf("comparison needs both challenges")
			}
			comparisons[id] = true
		}
		if len(c.ComparisonChallengeIDs) > 0 && (len(comparisons) < 2 || len(c.ComparisonChallengeIDs) > 8) {
			return fmt.Errorf("invalid comparison")
		}
		if c.Category == "industry_advice" && len(c.SourceURLs) == 0 {
			return fmt.Errorf("industry advice requires provenance")
		}
		for _, u := range c.SourceURLs {
			if !urls[u] {
				return fmt.Errorf("untrusted source URL")
			}
		}
	}
	return nil
}

func ValidateGrowthReport(report rc.GrowthReport, snapshot rc.Snapshot, sources []rc.Source) error {
	if report.SchemaVersion != 1 || len(report.Practices) > 3 || len(report.Architecture) > 8 {
		return fmt.Errorf("invalid growth report")
	}
	if err := ValidateGrowthClaims(report.Claims, sources, snapshot.SourceURLs); err != nil {
		return err
	}
	var overview struct {
		AuditComplete bool `json:"audit_complete"`
	}
	if json.Unmarshal(report.Overview, &overview) == nil && overview.AuditComplete && (!snapshot.AuditComplete || len(snapshot.AuditGaps) > 0) {
		return fmt.Errorf("model cannot promote audit completeness")
	}
	var ability map[string]json.RawMessage
	if err := json.Unmarshal(report.Ability, &ability); err != nil {
		return fmt.Errorf("invalid ability")
	}
	var level string
	if raw, ok := ability["overall_level"]; ok {
		if json.Unmarshal(raw, &level) != nil || level != "unknown" {
			return fmt.Errorf("model cannot assign ability level")
		}
	}
	owned := map[string]bool{}
	for _, s := range sources {
		owned[s.ID] = true
	}
	refsValid := func(refs []string) bool {
		if len(refs) < 1 || len(refs) > 8 {
			return false
		}
		for _, r := range refs {
			if !owned[r] {
				return false
			}
		}
		return true
	}
	textValid := func(values ...string) bool {
		for _, v := range values {
			if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > 2000 {
				return false
			}
		}
		return true
	}
	for _, a := range report.Architecture {
		if !refsValid(a.SourceRefs) || !textValid(a.Problem, a.Option, a.Preconditions, a.Benefits, a.Costs, a.Verification) {
			return fmt.Errorf("invalid architecture option")
		}
	}
	for _, p := range report.Practices {
		if !refsValid(p.SourceRefs) || !textValid(p.Dimension, p.Goal, p.SuccessCriteria) || len(p.Actions) < 1 || len(p.Actions) > 8 || !textValid(p.Actions...) {
			return fmt.Errorf("invalid growth practice")
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded) > 48*1024 {
		return fmt.Errorf("report exceeds budget")
	}
	return nil
}
