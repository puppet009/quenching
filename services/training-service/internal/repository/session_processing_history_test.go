package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/quenching/training-service/internal/service"
)

func TestMissingReviewLookupDoesNotClaimReportAbsent(t *testing.T) {
	rows := []service.SessionHistoryRow{{ID: 7}}
	if err := NewSessionRepo(nil, nil, nil, nil, nil).enrichReviewExistence(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rows)
	var wire []map[string]any
	_ = json.Unmarshal(raw, &wire)
	if wire[0]["reviewLookupFailed"] != true {
		t.Fatalf("unavailable review service reported as no report: %s", raw)
	}
}
