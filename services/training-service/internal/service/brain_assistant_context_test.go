package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAssistantNoGroundTruth(t *testing.T) {
	raw := `{"root_cause":"hidden-root","experiment_uid":"secret-uid","inject_fault":{"cpu_percent":80},"data":"student-visible","operation_type":"modify_config"}`
	ctx := PublicBrainObservation("e1", "terminal", raw)
	data, _ := json.Marshal(ctx)
	if strings.Contains(string(data), "hidden-root") || strings.Contains(string(data), "secret-uid") || strings.Contains(string(data), "cpu_percent") {
		t.Fatal("director truth leaked")
	}
}
