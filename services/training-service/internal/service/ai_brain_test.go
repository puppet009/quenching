package service

import (
	"github.com/quenching/training-service/internal/model"
	"testing"
)

func TestAIBrainRejectsStaleAndForeignEvidence(t *testing.T) {
	obs := model.BrainObservation{SessionID: 1, ObservationVersion: 2, EvidenceRefs: []string{"e1"}, AllowedTools: []*model.BrainToolDef{{Name: "wait_for_event", ParametersJson: `{"type":"object"}`}}}
	for _, raw := range []string{
		`{"decision_id":"d1","observation_version":1,"action":"wait_for_event","parameters_json":"{}","evidence_refs":["e1"]}`,
		`{"decision_id":"d1","observation_version":2,"action":"exec_shell","parameters_json":"{}"}`,
		`{"decision_id":"d1","observation_version":2,"action":"wait_for_event","parameters_json":"{}","evidence_refs":["foreign"]}`,
	} {
		if _, err := decodeBrainDecision(raw, obs); err == nil {
			t.Fatal("unsafe decision accepted")
		}
	}
	raw := `{"decision_id":"d1","observation_version":2,"learning_objective":"collect evidence","action":"wait_for_event","parameters_json":"{}","evidence_refs":["e1"],"reason_summary":"need regression","next_observation":"business_result"}`
	d, err := decodeBrainDecision(raw, obs)
	if err != nil || d.Action != "wait_for_event" {
		t.Fatalf("valid decision: %v", err)
	}
}
