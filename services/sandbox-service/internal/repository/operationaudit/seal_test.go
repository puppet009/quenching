package operationaudit

import (
	oetest "github.com/quenching/proto/operationevidence/testutil"
	"testing"
	"time"
)

func TestSealWaitsForWatermarkNotEmptyQueue(t *testing.T) {
	now := time.Now()
	s := AuditSeal{State: "pending", Watermark: 8, Acknowledged: 7, Deadline: now.Add(time.Minute)}
	v := EvaluateSeal(s, now)
	if v.State != "pending" {
		t.Fatal(v)
	}
	s.Acknowledged = 8
	if EvaluateSeal(s, now).State != "ready" {
		t.Fatal("not ready")
	}
}
func TestSealTimeoutProducesGap(t *testing.T) {
	now := time.Now()
	s := AuditSeal{Pending: 1, Watermark: 8, Acknowledged: 7, Deadline: now}
	v := EvaluateSeal(s, now)
	if v.State != "timed_out" || len(v.Gaps) == 0 {
		t.Fatal(v)
	}
}
func TestSealRejectsLateStartButAcceptsFinish(t *testing.T) {
	store := oetest.NewMemoryStore()
	e := Evidence{SchemaVersion: 1, OperationID: "o", EventID: "o:1", SessionID: 1, Sequence: 1, Actor: "student", Kind: "file", Status: "pending"}
	store.Begin(t.Context(), 1, "o", "o", "hash", e)
	store.Closed = true
	e = Next(e, "succeeded")
	if err := store.Append(t.Context(), e, nil); err != nil {
		t.Fatal(err)
	}
	e.OperationID = "late"
	e.EventID = "late:1"
	e.Sequence = 1
	e.Status = "pending"
	if _, err := store.Begin(t.Context(), 1, "late", "late", "hash", e); err != ErrClosed {
		t.Fatal(err)
	}
}
