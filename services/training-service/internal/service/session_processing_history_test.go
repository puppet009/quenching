package service

import (
	"context"
	"encoding/json"
	"testing"
)

type processingHistoryStore struct {
	SessionStore
	rows  []SessionHistoryRow
	owner int64
}

func (s *processingHistoryStore) ListSessions(_ context.Context, f SessionListFilter) ([]SessionHistoryRow, int64, error) {
	s.owner = f.UserID
	return s.rows, int64(len(s.rows)), nil
}

func TestHistoryRetainsDurableBackgroundStateBeforeReportExists(t *testing.T) {
	var rows []SessionHistoryRow
	if err := json.Unmarshal([]byte(`[{"id":7,"status":"completed","brainPhase":"closing","cleanupStatus":"pending","hasReview":false},{"id":8,"status":"aborted","reviewLookupFailed":true,"hasReview":false}]`), &rows); err != nil {
		t.Fatal(err)
	}
	store := &processingHistoryStore{rows: rows}
	page, err := NewSessionService(store, nil, nil).ListHistory(context.Background(), 17, SessionListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if store.owner != 17 {
		t.Fatal("history did not use authenticated user")
	}
	raw, _ := json.Marshal(page)
	var wire struct {
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.List[0]["status"] != "completed" || wire.List[0]["brainPhase"] != "closing" || wire.List[0]["cleanupStatus"] != "pending" || wire.List[1]["reviewLookupFailed"] != true {
		t.Fatalf("closing training disappeared behind absent review: %s", raw)
	}
}
