package service

import (
	"encoding/json"
	"fmt"
	"github.com/quenching/review-service/internal/model"
	"strings"
)

func ValidateBrainReviewClaims(claims []model.BrainReviewClaim, brain *model.BrainReviewInput) error {
	if brain == nil {
		return nil
	}
	if len(claims) == 0 {
		return fmt.Errorf("brain review requires evidence claims")
	}
	refs := map[string]bool{}
	for _, e := range brain.Evidence {
		refs[e.ID] = true
	}
	sources := map[string]bool{}
	for _, s := range brain.Sources {
		sources[s] = true
	}
	for _, c := range claims {
		if c.Section != "" && !brainClaimSections[c.Section] {
			return fmt.Errorf("unknown claim section")
		}
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("empty claim")
		}
		if c.Category != "observed" && c.Category != "industry_advice" {
			return fmt.Errorf("unknown claim category")
		}
		if c.Category == "observed" && len(c.EvidenceRefs) == 0 {
			return fmt.Errorf("observation lacks evidence")
		}
		for _, id := range c.EvidenceRefs {
			if !refs[id] {
				return fmt.Errorf("unknown evidence reference: %s", id)
			}
		}
		for _, url := range c.SourceURLs {
			if !sources[url] {
				return fmt.Errorf("unverified source URL")
			}
		}
	}
	return nil
}
func appendBrainClaims(raw string, brain *model.BrainReviewInput, sections *model.ReviewSections) error {
	if brain == nil {
		return nil
	}
	var body struct {
		Claims []model.BrainReviewClaim `json:"claims"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return err
	}
	if err := ValidateBrainReviewClaims(body.Claims, brain); err != nil {
		return err
	}
	// Brain reports render only typed validated claims; uncited model chapter prose is discarded.
	parts := map[string]string{}
	for key := range brainClaimSections {
		parts[key] = "未观察到足够证据，无法形成确定结论。"
	}
	seen := map[string]bool{}
	for _, c := range body.Claims {
		key := c.Section
		if key == "" {
			key = "knowledge"
		}
		if !seen[key] {
			parts[key] = ""
			seen[key] = true
		}
		label := "延伸建议"
		if c.Category == "observed" {
			label = "本次观察"
		}
		parts[key] += "\n- [" + label + "] " + c.Text
		if len(c.EvidenceRefs) > 0 {
			parts[key] += "（证据：" + strings.Join(c.EvidenceRefs, ", ") + "）"
		}
		for _, url := range c.SourceURLs {
			parts[key] += " " + url
		}
	}
	sections.RootCause = parts["root_cause"]
	sections.Timeline = parts["timeline"]
	sections.PathComparison = parts["path_comparison"]
	sections.HighlightsPitfalls = parts["highlights_pitfalls"]
	sections.Methodology = parts["methodology"]
	sections.Knowledge = parts["knowledge"]
	return nil
}

var brainClaimSections = map[string]bool{"root_cause": true, "timeline": true, "path_comparison": true, "highlights_pitfalls": true, "methodology": true, "knowledge": true}
