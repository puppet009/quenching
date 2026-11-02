package operationaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func operationDB(t *testing.T) *PGStore {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent; PostgreSQL not verified")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("operation_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close(); admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	raw, err := os.ReadFile("../../../migrations/000009_operation_evidence.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	return &PGStore{Pool: p}
}
func TestPGOperationBeginRace(t *testing.T) {
	s := operationDB(t)
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := Evidence{SchemaVersion: 1, EventID: "o:1", OperationID: "o", SessionID: 1, Sequence: 1, Actor: "student", Kind: "command", Status: "pending"}
			ok, err := s.Begin(context.Background(), 1, "o", "key", "hash", e)
			if err != nil {
				t.Error(err)
			}
			if ok {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
}
func TestPGIngestAtomicWithText(t *testing.T) {
	s := operationDB(t)
	s.Local = true
	e := Evidence{SchemaVersion: 1, EventID: "o:2", OperationID: "o", SessionID: 1, Sequence: 2, Actor: "student", Kind: "command", Status: "succeeded"}
	e.Arguments = json.RawMessage(`{"z":1,"a":{"q":true,"b":2}}`)
	e.Result = json.RawMessage(`{"result":true,"detail":{"z":1,"a":2}}`)
	d := Delivery{e, []Text{CaptureText("o", "stdout", []byte("tail"), TextLimit)}}
	if err := s.Ingest(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(context.Background(), d); err != nil {
		t.Fatal("duplicate", err)
	}
	v, err := s.Read(context.Background(), 1, "optext:o:stdout")
	if err != nil || v.Content != "tail" {
		t.Fatal(v, err)
	}
	d.Evidence.EventID = "o:3"
	d.Evidence.Sequence = 3
	d.Texts[0].OperationID = "foreign"
	if s.Ingest(context.Background(), d) == nil {
		t.Fatal("foreign text accepted")
	}
	var n int
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM sandbox_operation_events WHERE event_id='o:3'`).Scan(&n)
	if n != 0 {
		t.Fatal("partial event committed")
	}
}

func TestPGSealBeginSerialization(t *testing.T) {
	s := operationDB(t)
	s.Local = true
	ctx := context.Background()
	e := Evidence{SchemaVersion: 1, EventID: "o:1", OperationID: "o", SessionID: 1, Sequence: 1, Actor: "student", Kind: "command", Status: "pending"}
	if _, err := s.Begin(ctx, 1, "o", "o", "hash", e); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	seal, err := s.Seal(ctx, 1, "test", now)
	if err != nil || seal.State != "pending" {
		t.Fatal(seal, err)
	}
	late := e
	late.OperationID = "late"
	late.EventID = "late:1"
	if _, err = s.Begin(ctx, 1, "late", "late", "hash", late); err != ErrClosed {
		t.Fatal(err)
	}
	if err = s.Append(ctx, Next(e, "succeeded"), nil); err != nil {
		t.Fatal(err)
	}
	seal, err = s.Seal(ctx, 1, "test", now)
	if err != nil || seal.State != "ready" {
		t.Fatal(seal, err)
	}
}

func TestPGCanonicalStoreSharesSealAcrossClients(t *testing.T) {
	local := operationDB(t)
	local.Local = true
	remote := &PGStore{Pool: local.Pool, Sandbox: true, Local: true}
	ctx := context.Background()
	e := Evidence{SchemaVersion: 1, SessionID: 1, OperationID: "same", EventID: "same:1", Sequence: 1, Actor: "student", Kind: "command", Status: "pending"}
	if _, err := local.Begin(ctx, 1, "same", "key", "hash", e); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Seal(ctx, 1, "sandbox", time.Now()); err != nil {
		t.Fatal(err)
	}
	e.OperationID = "another"
	e.EventID = "another:1"
	if _, err := local.Begin(ctx, 1, "another", "another", "hash", e); err != ErrClosed {
		t.Fatal("canonical seal did not close second client", err)
	}
}
