package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPublicOperationKeepsSafeTargetAndDetail(t *testing.T) {
	v := publicMemoryRawSource("report:1", "operation", "student_report", `{"operation_type":"command","target":"database","detail":{"command":"restart database","question":"password=secretValue","root_cause":"hiddenAnswer"}}`, time.Now(), "")
	if !strings.Contains(v.Text, "restart database") || !strings.Contains(v.Text, "database") || strings.Contains(v.Text, "secretValue") || strings.Contains(v.Text, "hiddenAnswer") {
		t.Fatal(v.Text)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"actor":"student_report"`) || v.Role != "user" || v.Successful {
		t.Fatal(string(raw))
	}
}
