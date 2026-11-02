package repository

import (
	"encoding/json"
	"errors"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/training-service/internal/service"
	"time"
)

func reviewAuditSource(id, kind, actor, raw string, at time.Time) (rc.Source, error) {
	if (kind != "operation" || actor != "student_report") && ((kind != "verification" && kind != "assistant_file_change") || actor != "platform") {
		return rc.Source{}, errors.New("private review audit source")
	}
	if !json.Valid([]byte(raw)) {
		return rc.Source{}, errors.New("invalid review audit JSON")
	}
	src := rc.Source{ID: id, Kind: kind, Actor: actor, Text: service.RedactSessionText(raw), OccurredAt: at}
	if kind == "verification" {
		var result struct {
			Status     string `json:"status"`
			ResultJSON string `json:"result_json"`
		}
		_ = json.Unmarshal([]byte(raw), &result)
		var facts struct {
			BusinessVerified bool   `json:"business_verified"`
			ExecutionStatus  string `json:"execution_status"`
			Truncated        bool   `json:"truncated"`
		}
		if result.ResultJSON != "" {
			_ = json.Unmarshal([]byte(result.ResultJSON), &facts)
		} else {
			_ = json.Unmarshal([]byte(raw), &facts)
		}
		src.Incomplete = facts.Truncated || facts.ExecutionStatus == "unknown"
		src.Successful = result.Status == "succeeded" && facts.BusinessVerified && !src.Incomplete
	}
	return src, nil
}
