package service

import (
	"context"
	"github.com/quenching/training-service/internal/model"
	"testing"
)

// These are contract fixtures, never a claim of live server acceptance.
func TestBrainClosedLoopWithRecordedDomainResults(t *testing.T) {
	state := BrainSessionState{SessionID: 7, Phase: model.BrainBaselineCheck}
	inject := model.BrainDecision{Action: "inject_fault"}
	if ValidateDirectorAction(state, inject) == nil {
		t.Fatal("baseline bypass")
	}
	state.BaselineVerified = true
	if err := ValidateCalibrationFaults(0, []int64{1}, []BrainTrainableFault{{DefectID: 1, Difficulty: "easy"}}); err != nil {
		t.Fatal(err)
	}
	state.CurrentChallenge = 1
	state.ActiveChallenge = true
	if ValidateDirectorAction(state, inject) == nil {
		t.Fatal("overlapping challenge")
	}
	// Business recovery can be verified while the injected experiment still exists.
	state.ActiveChallenge = false
	state.PendingFaultRevoke = true
	if ValidateDirectorAction(state, inject) == nil {
		t.Fatal("new challenge before previous experiment revoked")
	}
	state.PendingFaultRevoke = false
	if ValidateDirectorAction(state, inject) != nil {
		t.Fatal("next challenge rejected after revoke")
	}
	n, err := NextBrainChallenge(state.CurrentChallenge, false)
	if err != nil || n != 2 {
		t.Fatal("same-session sequence lost")
	}
	s := &cleanupTestStore{evidence: []string{"recorded:business-result"}}
	reviews := &cleanupTestReview{}
	close := NewBrainCleanupCoordinator(s, cleanupTestFault{}, cleanupTestSandbox{}, reviews)
	if err = close.Close(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if err = close.Close(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if reviews.calls != 1 || s.status != "succeeded" || len(s.evidence) != 1 {
		t.Fatal("closure is not idempotent or audit was lost")
	}
}
func TestDegradedWithoutTrainableFault(t *testing.T) {
	phase, reason, err := BrainCatalogAvailability(`{"faults":[],"combinations":[]}`)
	if err != nil || phase != model.BrainDegraded || reason != "no_verified_faults" {
		t.Fatalf("%s %s %v", phase, reason, err)
	}
	if _, _, err = BrainCatalogAvailability(`{"faults":"not an array"}`); err == nil {
		t.Fatal("malformed catalog accepted")
	}
}
