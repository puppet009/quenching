package repository

import (
	"strings"
	"testing"
	"time"
)

func TestReviewAuditSourcesKeepFullVerificationAndFileContent(t *testing.T) {
	for _, kind := range []string{"verification", "assistant_file_change", "operation"} {
		actor := "platform"
		if kind == "operation" {
			actor = "student_report"
		}
		raw := `{"business_verified":false,"result":"success","after_content":"` + strings.Repeat("x", 9000) + `TAIL_VERIFIED","password":"secret-value"}`
		src, err := reviewAuditSource("owned:1", kind, actor, raw, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(src.Text, "TAIL_VERIFIED") || strings.Contains(src.Text, "secret-value") {
			t.Fatal("tail or secret handling")
		}
		if src.Successful {
			t.Fatal("failed verification or self-report became success")
		}
	}
	if _, err := reviewAuditSource("private:1", "director_plan", "platform", `{}`, time.Now()); err == nil {
		t.Fatal("private director source admitted")
	}
}

func TestReviewVerificationRequiresBusinessProof(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		success bool
	}{
		{`{"status":"succeeded","result_json":"{\"business_verified\":true}"}`, true},
		{`{"status":"failed","result_json":"{\"business_verified\":true}"}`, false},
		{`{"status":"succeeded","result_json":"{\"business_verified\":true,\"truncated\":true}"}`, false},
	} {
		src, err := reviewAuditSource("tool:1", "verification", "platform", tc.raw, time.Now())
		if err != nil || src.Successful != tc.success {
			t.Fatalf("%s: %+v %v", tc.raw, src, err)
		}
	}
}
