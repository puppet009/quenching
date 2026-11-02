package operationaudit

import (
	"context"
	"encoding/json"
	"errors"
	oetest "github.com/quenching/proto/operationevidence/testutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestAuditDuplicateAndConflictingKey(t *testing.T) {
	s := oetest.NewMemoryStore()
	ctx := context.Background()
	e := Evidence{SchemaVersion: 1, OperationID: "o", EventID: "o:1", SessionID: 1, Sequence: 1, Actor: "student", Kind: "command", Status: "pending"}
	ok, err := s.Begin(ctx, 1, "o", "k", "hash", e)
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	ok, err = s.Begin(ctx, 1, "other", "k", "hash", e)
	if ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err = s.Begin(ctx, 1, "o", "k", "different", e); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
func TestEventSequenceNeverRegresses(t *testing.T) {
	s := oetest.NewMemoryStore()
	ctx := context.Background()
	e := Evidence{SchemaVersion: 1, OperationID: "o", EventID: "o:1", SessionID: 1, Sequence: 1, Actor: "student", Kind: "command", Status: "pending"}
	s.Begin(ctx, 1, "o", "k", "hash", e)
	e.Sequence = 2
	e.EventID = "o:2"
	e.Status = "succeeded"
	if err := s.Append(ctx, e, nil); err != nil {
		t.Fatal(err)
	}
	e.Sequence = 3
	e.EventID = "o:3"
	e.Status = "running"
	if s.Append(ctx, e, nil) == nil {
		t.Fatal("terminal regressed")
	}
}
func TestAuditConcurrentBeginOnce(t *testing.T) {
	s := oetest.NewMemoryStore()
	var wg sync.WaitGroup
	var mu sync.Mutex
	count := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := Evidence{SchemaVersion: 1, OperationID: "o", EventID: "o:1", SessionID: 1, Sequence: 1, Actor: "student", Kind: "command", Status: "pending"}
			ok, err := s.Begin(context.Background(), 1, "o", "k", "hash", e)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if count != 1 {
		t.Fatal(count)
	}
}
func TestAuditOutboxRequiresAcknowledgement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]int{"code": 9}) }))
	defer srv.Close()
	if Deliver(context.Background(), srv.Client(), srv.URL, "token", Delivery{}) == nil {
		t.Fatal("bad ack accepted")
	}
}
