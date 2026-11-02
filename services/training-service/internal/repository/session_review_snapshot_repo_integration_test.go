package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/training-service/internal/service"
	"os"
	"strings"
	"testing"
)

type frozenInputFake struct{ service.ReviewInputStore }

func (frozenInputFake) GetReviewInput(_ context.Context, s int64) (*service.SessionReviewInput, error) {
	return &service.SessionReviewInput{SessionID: s}, nil
}
func TestPGSnapshotFreezeIsStableAndAggregatesHistory(t *testing.T) {
	ctx, _, pool := sessionMemoryDB(t)
	for _, name := range []string{"000006_create_session_operations", "000018_create_ai_change_records", "000025_session_review_snapshot"} {
		applySnapshotMigration(t, ctx, pool, name)
	}
	if _, e := pool.Exec(ctx, `UPDATE training_sessions SET status='completed' WHERE id=1; INSERT INTO ai_conversations(id,user_id,session_id,conversation_type,status) VALUES(10,10,1,'training_assistant','active'),(11,10,1,'training_assistant','active')`); e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 65; i++ {
		if _, e := pool.Exec(ctx, `INSERT INTO ai_messages(conversation_id,role,content) VALUES($1,'user',$2)`, int64(10+i%2), fmt.Sprint(i)); e != nil {
			t.Fatal(e)
		}
	}
	repo := NewSessionReviewSnapshotRepo(pool, frozenInputFake{})
	snap, e := repo.Freeze(ctx, 10, 1, 0)
	if e != nil || snap.SourceCount != 65 {
		t.Fatal(snap, e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO ai_messages(conversation_id,role,content) VALUES(10,'user','late audit')`); e != nil {
		t.Fatal(e)
	}
	again, e := repo.Freeze(ctx, 10, 1, 0)
	if e != nil || again.Hash != snap.Hash || again.SourceCount != 65 {
		t.Fatal(again, e)
	}
	page, e := repo.Sources(ctx, 10, 1, 1, rc.Cursor{}, 48*1024)
	if e != nil || len(page.Fragments) != 65 || !page.Done {
		t.Fatal(page, e)
	}
	if _, e = repo.Manifest(ctx, 11, 1); e == nil {
		t.Fatal("foreign snapshot")
	}
}
func TestPGSnapshotReadsOwnedLongSource(t *testing.T) {
	ctx, _, pool := sessionMemoryDB(t)
	applySnapshotMigration(t, ctx, pool, "000006_create_session_operations")
	applySnapshotMigration(t, ctx, pool, "000018_create_ai_change_records")
	applySnapshotMigration(t, ctx, pool, "000025_session_review_snapshot")
	if _, e := pool.Exec(ctx, `UPDATE training_sessions SET status='completed' WHERE id=1; INSERT INTO ai_conversations(id,user_id,session_id,conversation_type,status) VALUES(10,10,1,'training_assistant','active')`); e != nil {
		t.Fatal(e)
	}
	var id int64
	e := pool.QueryRow(ctx, `INSERT INTO ai_messages(conversation_id,role,content) VALUES(10,'user',$1) RETURNING id`, strings.Repeat("x", 9000)+"important tail").Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	repo := NewSessionReviewSnapshotRepo(pool, frozenInputFake{})
	snap, e := repo.Freeze(ctx, 10, 1, 0)
	if e != nil {
		t.Fatal(e)
	}
	first, e := repo.Read(ctx, 10, 1, snap.Version, fmt.Sprintf("message:%d", id), 0, 8192)
	if e != nil || first.Complete {
		t.Fatal(first, e)
	}
	last, e := repo.Read(ctx, 10, 1, 1, first.Source.ID, first.NextOffsetBytes, 8192)
	if e != nil || !last.Complete || !strings.Contains(last.Source.Text, "important tail") {
		t.Fatal(last, e)
	}
	if _, e = repo.Read(ctx, 11, 1, 1, first.Source.ID, 0, 8192); e == nil {
		t.Fatal("foreign source")
	}
	raw, _ := json.Marshal(last)
	if !json.Valid(raw) {
		t.Fatal("invalid fragment")
	}
}

func applySnapshotMigration(t *testing.T, ctx context.Context, p *pgxpool.Pool, name string) {
	t.Helper()
	b, e := os.ReadFile("../../migrations/" + name + ".up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, string(b)); e != nil {
		t.Fatal(e)
	}
}

func TestPGSnapshotFreezesAuditOriginals(t *testing.T) {
	ctx, _, pool := sessionMemoryDB(t)
	for _, name := range []string{"000006_create_session_operations", "000018_create_ai_change_records", "000025_session_review_snapshot"} {
		applySnapshotMigration(t, ctx, pool, name)
	}
	_, err := pool.Exec(ctx, `UPDATE training_sessions SET status='completed' WHERE id=1;
 INSERT INTO training_operation_projection(session_id,operation_type,detail,result) VALUES(1,'exec_command','{"command":"check orders"}','success');
 INSERT INTO ai_change_records(session_id,pod,path,after_content) VALUES(1,'mall','order.go','frozen file tail');
 INSERT INTO brain_sessions(session_id,phase,cleanup_status) VALUES(1,'completed','succeeded');
 INSERT INTO brain_decisions(decision_id,session_id,observation_version,decision,actual_model,prompt_version) VALUES('audit-d',1,1,'{}','qwen3.8-flash','test');
 INSERT INTO brain_tool_executions(command_id,session_id,decision_id,observation_version,domain,action,parameters,status,result) VALUES('audit-tool',1,'audit-d',1,'simulator','verify_business','{}','succeeded','{"status":"succeeded","result_json":"{\"business_verified\":false,\"output\":\"failed order validation\"}"}');`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSessionReviewSnapshotRepo(pool, frozenInputFake{})
	snap, err := repo.Freeze(ctx, 10, 1, 0)
	if err != nil || snap.SourceCount != 3 {
		t.Fatal(snap, err)
	}
	page, err := repo.Sources(ctx, 10, 1, 1, rc.Cursor{}, 48*1024)
	if err != nil || !page.Done {
		t.Fatal(page, err)
	}
	kinds := map[string]bool{}
	for _, f := range page.Fragments {
		kinds[f.Source.Kind] = true
		if f.Source.Kind == "verification" && (!strings.Contains(f.Source.Text, "failed order validation") || f.Source.Successful) {
			t.Fatal(f)
		}
		if f.Source.Kind == "operation" && f.Source.Trust != "user_statement" {
			t.Fatal(f)
		}
	}
	for _, kind := range []string{"operation", "assistant_file_change", "verification"} {
		if !kinds[kind] {
			t.Fatal("missing", kind)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO ai_change_records(session_id,pod,path,after_content) VALUES(1,'mall','order.go','late change')`); err != nil {
		t.Fatal(err)
	}
	again, err := repo.Freeze(ctx, 10, 1, 0)
	if err != nil || again.Hash != snap.Hash || again.SourceCount != 3 {
		t.Fatal(again, err)
	}
}
