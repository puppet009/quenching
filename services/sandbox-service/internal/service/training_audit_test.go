package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditRedactsSecretsAndMarksUnknownTerminal(t *testing.T) {
	raw, e := BuildTrainingAuditEvent("a1", 7, "terminal", `{"direction":"input","data":"curl -H 'Authorization: Bearer secret-token' /api; password=secret-pass"}`)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "secret-token") || strings.Contains(string(raw), "secret-pass") {
		t.Fatal("audit contains credentials")
	}
	var v map[string]any
	json.Unmarshal(raw, &v)
	if !strings.Contains(string(raw), "unknown") {
		t.Fatal("terminal claimed known exit code")
	}
}

func TestAuditNestedCredentialKeys(t *testing.T) {
	raw, _ := BuildTrainingAuditEvent("a2", 7, "file_diff", `{"db_password":"hidden-value","payment_secret":"hidden-value"}`)
	if strings.Contains(string(raw), "hidden-value") {
		t.Fatal("secret retained")
	}
}

type closedAuditStore struct{ TrainingAuditStore }

func (closedAuditStore) WorkspaceOpen(context.Context, int64) (bool, error) { return false, nil }
func TestClosedWorkspaceRejectsNewInput(t *testing.T) {
	if NewTrainingAuditService(closedAuditStore{}).AssertWorkspaceOpen(context.Background(), 7) == nil {
		t.Fatal("closed workspace accepted input")
	}
}

func TestAuditQuotedConfigurationSecrets(t *testing.T) {
	input, err := json.Marshal(map[string]string{"content": "password: \"example-secret\"\nTOKEN='another-secret'"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := BuildTrainingAuditEvent("q1", 7, "file_diff", string(input))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "example-secret") || strings.Contains(string(raw), "another-secret") {
		t.Fatal("quoted secret retained")
	}
}
