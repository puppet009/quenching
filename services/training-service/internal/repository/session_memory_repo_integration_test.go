package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/training-service/internal/model"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func sessionMemoryDB(t *testing.T) (context.Context, *SessionMemoryRepo, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit TEST_DATABASE_URL required; PostgreSQL not verified")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	var nonce [8]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		t.Fatal(e)
	}
	schema := "memory_test_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		_, e := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		if e != nil {
			t.Error(e)
		}
		admin.Close(ctx)
	})
	for _, name := range []string{"000001_create_ai_conversations", "000002_create_ai_messages", "000003_create_training_sessions", "000006_create_session_operations", "000013_add_ai_message_hints", "000020_create_brain_state", "000021_brain_runtime_projection", "000022_brain_cleanup", "000023_brain_challenge_revoke", "000024_session_assistant_memory", "000026_operation_evidence", "000027_operation_ownership"} {
		b, e := os.ReadFile("../../migrations/" + name + ".up.sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(b)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.Exec(ctx, `INSERT INTO training_sessions(id,user_id,project_id,status) VALUES(1,10,0,'running')`); e != nil {
		t.Fatal(e)
	}
	return ctx, NewSessionMemoryRepo(pool), pool
}
func TestResolveConcurrentCanonicalConversation(t *testing.T) {
	ctx, r, _ := sessionMemoryDB(t)
	var wg sync.WaitGroup
	ids := make(chan int64, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := r.Resolve(ctx, 10, 1)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- c.ConversationID
		}()
	}
	wg.Wait()
	close(ids)
	var first int64
	for id := range ids {
		if first == 0 {
			first = id
		}
		if id != first {
			t.Fatal("different canonical IDs")
		}
	}
	if first == 0 {
		t.Fatal("no conversation")
	}
	if _, e := r.Resolve(ctx, 11, 1); e == nil {
		t.Fatal("foreign session allowed")
	}
}
func TestLegacyConversationHistoryStablePagination(t *testing.T) {
	ctx, r, p := sessionMemoryDB(t)
	_, e := p.Exec(ctx, `INSERT INTO ai_conversations(id,session_id,user_id) VALUES(10,1,10),(11,1,10);INSERT INTO ai_messages(id,conversation_id,role,content) VALUES(100,10,'user','first'),(101,11,'assistant','second'),(102,10,'user','third')`)
	if e != nil {
		t.Fatal(e)
	}
	c, e := r.Resolve(ctx, 10, 1)
	if e != nil || c.ConversationID != 10 {
		t.Fatalf("legacy canonical %v %v", c, e)
	}
	page, e := r.History(ctx, 10, 1, 0, 2)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Messages) != 2 || page.Messages[0].ID != 101 || page.NextBeforeID != 101 || !page.HasMore {
		t.Fatalf("bad page %#v", page)
	}
	older, e := r.History(ctx, 10, 1, page.NextBeforeID, 2)
	if e != nil || len(older.Messages) != 1 || older.Messages[0].ID != 100 {
		t.Fatal(older, e)
	}
}
func TestMemoryCASRejectsStaleVersion(t *testing.T) {
	ctx, r, _ := sessionMemoryDB(t)
	_, e := r.Resolve(ctx, 10, 1)
	if e != nil {
		t.Fatal(e)
	}
	l, e := r.Acquire(ctx, 10, 1, "worker", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	next := model.SessionMemory{MemoryJSON: `{"entries":[]}`, MessageWatermark: 0}
	ok, e := r.SaveCAS(ctx, 10, 1, 0, l, next)
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	ok, e = r.SaveCAS(ctx, 10, 1, 0, l, next)
	if e != nil || ok {
		t.Fatal("stale CAS", ok, e)
	}
}
func TestExpiredLeaseCannotCommit(t *testing.T) {
	ctx, r, p := sessionMemoryDB(t)
	_, e := r.Resolve(ctx, 10, 1)
	if e != nil {
		t.Fatal(e)
	}
	l, e := r.Acquire(ctx, 10, 1, "worker", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(ctx, `UPDATE session_assistant_identity SET lease_expires=now()-interval '1 second'`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = r.AppendLeasedMessage(ctx, 10, 1, l, model.SessionMessage{Role: "user", Content: "late"})
	if e == nil {
		t.Fatal("late write accepted")
	}
	newLease, e := r.Acquire(ctx, 10, 1, "next", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if newLease.Fence <= l.Fence {
		t.Fatal("fence not incremented")
	}
	ok, e := r.Renew(ctx, 10, 1, l, time.Minute)
	if e != nil || ok {
		t.Fatal("old lease renewed")
	}
}

func TestPGMemorySourcesExcludePrivateEvidenceAndRanges(t *testing.T) {
	ctx, r, p := sessionMemoryDB(t)
	_, e := r.Resolve(ctx, 10, 1)
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(ctx, `INSERT INTO brain_sessions(session_id) VALUES(1); INSERT INTO brain_evidence_refs(id,session_id,kind,actor,occurred_at,payload) VALUES('public',1,'student_operation','student',now(),'{"result":"success","data":"visible","root_cause":"hidden"}'),('private',1,'injection','fault_engine',now(),'{"root_cause":"hidden"}')`)
	if e != nil {
		t.Fatal(e)
	}
	page, e := r.Sources(ctx, 10, 1, 0, 0, 100)
	if e != nil || len(page.Sources) != 1 || page.Sources[0].ID != "public" || strings.Contains(page.Sources[0].Text, "hidden") {
		t.Fatal(page, e)
	}
	if _, e = r.ReadReferences(ctx, 10, 1, nil, []string{"private"}); e == nil {
		t.Fatal("private reference readable")
	}
	if _, e = r.ReadSourceRange(ctx, 10, 1, "private", 0, 100); e == nil {
		t.Fatal("private range readable")
	}
}
func TestPGSessionSearchEscapesWildcards(t *testing.T) {
	ctx, r, p := sessionMemoryDB(t)
	c, e := r.Resolve(ctx, 10, 1)
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(ctx, `INSERT INTO ai_messages(conversation_id,role,content) VALUES($1,'user','literal 10% battery'),($1,'user','unrelated text')`, c.ConversationID)
	if e != nil {
		t.Fatal(e)
	}
	page, e := r.Search(ctx, 10, 1, "%", 0, 20)
	if e != nil || len(page.Messages) != 1 {
		t.Fatal(page, e)
	}
}
