package service

import (
	"github.com/quenching/review-service/internal/model"
	"strings"
	"testing"
)

func TestReviewEvidenceReferences(t *testing.T) {
	brain := &model.BrainReviewInput{Evidence: []model.BrainReviewEvidence{{ID: "e1"}}}
	if ValidateBrainReviewClaims([]model.BrainReviewClaim{{Category: "observed", Text: "user repaired", EvidenceRefs: []string{"foreign"}}}, brain) == nil {
		t.Fatal("uncited fact accepted")
	}
	if ValidateBrainReviewClaims([]model.BrainReviewClaim{{Category: "industry_advice", Text: "consider isolating the cache"}}, brain) != nil {
		t.Fatal("labelled suggestion needs no invented runtime fact")
	}
}

func TestBrainFreeTextCannotBypassEvidenceClaims(t *testing.T) {
	brain := &model.BrainReviewInput{Evidence: []model.BrainReviewEvidence{{ID: "e1"}}}
	sections := &model.ReviewSections{RootCause: "fabricated repair"}
	raw := `{"claims":[{"category":"industry_advice","text":"consider isolation"}]}`
	if err := appendBrainClaims(raw, brain, sections); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sections.RootCause, "fabricated repair") {
		t.Fatal("uncited free text displayed as fact")
	}
}
