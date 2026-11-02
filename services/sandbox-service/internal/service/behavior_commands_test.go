package service

import (
	"github.com/quenching/sandbox-service/internal/model"
	"testing"
)

func TestDuplicateBehaviorCommand(t *testing.T) {
	a := model.BehaviorCommand{CommandID: "c1", SessionID: 1, TemplateSlug: "mall", Operation: "order", ParametersJSON: `{"profile":"steady"}`}
	b := a
	b.ParametersJSON = `{ "profile": "steady" }`
	if !SameBehaviorCommand(a, b) {
		t.Fatal("JSON formatting broke idempotence")
	}
	b.Operation = "pay"
	if SameBehaviorCommand(a, b) {
		t.Fatal("command id reused for other operation")
	}
}
func TestResultLostThenRecovered(t *testing.T) {
	if BehaviorCommandMayRepeat("running") || BehaviorCommandMayRepeat("unknown") {
		t.Fatal("ambiguous execution repeated")
	}
}
func TestWrongRunnerSession(t *testing.T) {
	cmd := model.BehaviorCommand{CommandID: "c1", SessionID: 1, TemplateSlug: "mall", Operation: "order", ParametersJSON: "{}"}
	if ValidateBehaviorCommand(2, cmd) == nil {
		t.Fatal("foreign command accepted")
	}
	cmd.ParametersJSON = `{"entry_url":"https://external.example"}`
	if ValidateBehaviorCommand(1, cmd) == nil {
		t.Fatal("external target accepted")
	}
}
