package service

import (
	"github.com/quenching/training-service/internal/model"
	"testing"
)

func TestFastFixDoesNotPromote(t *testing.T) {
	a := &model.AbilityAssessment{OverallLevel: "intermediate", Sufficiency: "sufficient", Dimensions: map[string]model.DimensionAssessment{}}
	if GuardAbilityPromotion(a, 1, 1); a.OverallLevel != "unknown" {
		t.Fatal("one fast CPU fix promoted developer")
	}
}
func TestAutoCleanupNotUserFix(t *testing.T) {
	if CountVerifiedChallenges([]BrainChallengeFact{{Status: "closed_unresolved", DefectIDs: []int64{1}}}) != 0 {
		t.Fatal("automatic cleanup counted as repair")
	}
}
