package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	oe "github.com/quenching/proto/operationevidence"
	"os"
	"testing"
	"time"
)

func TestPGOperationOwnershipPreservesHistoryAndReportIdempotency(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent; PostgreSQL migration not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("domain_ops_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_, e := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		if e != nil {
			t.Error(e)
		}
	}()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if _, e = pool.Exec(ctx, `CREATE TABLE training_sessions(id BIGSERIAL PRIMARY KEY);INSERT INTO training_sessions VALUES(41);CREATE TABLE brain_challenges(id BIGSERIAL PRIMARY KEY);`); e != nil {
		t.Fatal(e)
	}
	run := func(path string) {
		t.Helper()
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatalf("%s: %v", path, e)
		}
	}
	run("../../../training-service/migrations/000006_create_session_operations.up.sql")
	run("../../../training-service/migrations/000026_operation_evidence.up.sql")
	run("../../migrations/000009_operation_evidence.up.sql")
	if _, e = pool.Exec(ctx, `INSERT INTO session_operations(id,session_id,operation_type,detail,deleted_at) VALUES(71,41,'view_log','{"evidence":"retained"}',now());INSERT INTO operation_intents(operation_id,session_id,request_key,request_hash,evidence) VALUES('tool',41,'key','hash','{"actor":"assistant","kind":"tool"}');INSERT INTO operation_events(event_id,operation_id,session_id,sequence,evidence,delivery) VALUES('tool:1','tool',41,1,'{"actor":"assistant","kind":"tool"}','{}');INSERT INTO operation_texts VALUES('text','tool',41,'{"content":"retained"}');`); e != nil {
		t.Fatal(e)
	}
	run("../../../training-service/migrations/000027_operation_ownership.up.sql")
	run("../../migrations/000012_operation_ownership.up.sql")
	for _, table := range []string{"session_operations", "sandbox_session_operations", "training_operation_projection"} {
		var id int64
		var deleted *time.Time
		if e = pool.QueryRow(ctx, "SELECT id,deleted_at FROM "+table).Scan(&id, &deleted); e != nil || id != 71 || deleted == nil {
			t.Fatalf("%s lost history: %v", table, e)
		}
	}
	for _, table := range []string{"sandbox_operation_intents", "training_tool_intents", "training_operation_intents"} {
		var id string
		if e = pool.QueryRow(ctx, "SELECT operation_id FROM "+table).Scan(&id); e != nil || id != "tool" {
			t.Fatalf("%s lost tool: %v", table, e)
		}
	}
	repo := NewLegacyOperationRepo(pool)
	b := oe.LegacyBatch{RequestID: "once", SessionID: 41, UserID: 2, Operations: []oe.LegacyInput{{Type: "view_log", VirtualTimestamp: -1}}}
	first, e := repo.Record(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	again, e := repo.Record(ctx, b)
	if e != nil || first != again || first.Written != 1 {
		t.Fatalf("duplicate changed: %v %v %v", first, again, e)
	}
	var id int64
	if e = pool.QueryRow(ctx, "SELECT max(id) FROM sandbox_session_operations").Scan(&id); e != nil || id != 72 {
		t.Fatalf("source sequence: %d %v", id, e)
	}
	b.Operations[0].Type = "restart_service"
	if _, e = repo.Record(ctx, b); e != oe.ErrConflict {
		t.Fatalf("conflicting duplicate accepted: %v", e)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM training_operation_projection").Scan(&count); e != nil || count != 1 {
		t.Fatal("source write mutated training projection")
	}
}
