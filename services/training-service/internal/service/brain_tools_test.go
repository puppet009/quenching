package service

import (
	"github.com/quenching/training-service/internal/model"
	"testing"
)

func TestRejectArbitraryShell(t *testing.T) {
	for _, cmd := range []model.BrainCommand{
		{SessionID: 1, Action: "exec_shell", ParametersJSON: `{"command":"rm -rf /"}`},
		{SessionID: 1, Action: "inject_fault", ParametersJSON: `{"defect_ids":[1],"target_namespace":"production"}`},
		{SessionID: 1, Action: "run_user_behavior", ParametersJSON: `{"operation":"order","entry_url":"https://external.example"}`},
	} {
		if _, err := ValidateBrainToolParameters(cmd); err == nil {
			t.Fatal("arbitrary execution accepted")
		}
	}
}
