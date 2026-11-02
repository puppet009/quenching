package service

import (
	"encoding/json"
	"github.com/quenching/sandbox-service/internal/model"
	"testing"
)

func TestBehaviorOutboxEvidenceIsStableAndScoped(t *testing.T) {
	body, err := BuildBehaviorEvidenceEvent(1, model.BehaviorResult{CommandID: "c1", Status: "succeeded", ResultJSON: `{"order_id":8}`})
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if json.Unmarshal(body, &data) != nil {
		t.Fatal("invalid event")
	}
	event := data["event"].(map[string]any)
	if event["event_id"] != "behavior:c1:succeeded" || event["session_id"] != float64(1) {
		t.Fatal("event scope/dedup lost")
	}
	if _, err = BuildBehaviorEvidenceEvent(0, model.BehaviorResult{}); err == nil {
		t.Fatal("invalid evidence identity accepted")
	}
}
