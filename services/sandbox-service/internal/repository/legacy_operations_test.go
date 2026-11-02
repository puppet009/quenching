package repository

import (
	"context"
	oe "github.com/quenching/proto/operationevidence"
	"testing"
)

func TestLegacyReportRejectsEmptyOperationBeforePersistence(t *testing.T) {
	r := NewLegacyOperationRepo(nil)
	if _, err := r.Record(context.Background(), oe.LegacyBatch{RequestID: "request", SessionID: 1, UserID: 2, Operations: []oe.LegacyInput{{Type: " "}}}); err == nil {
		t.Fatal("empty operation accepted")
	}
}
