package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPGConfigurationOwnershipPreservesIDsEvidenceAndSequence(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("domain_config_%d", time.Now().UnixNano())
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
	_, e = pool.Exec(ctx, `CREATE TABLE defects(id BIGSERIAL PRIMARY KEY,name TEXT,provenance JSONB,deleted_at TIMESTAMPTZ);
 INSERT INTO defects(id,name,provenance) VALUES(11,'first','{"url":"incident"}'),(17,'second','{}');
 CREATE TABLE ai_pitfall_configs(id BIGSERIAL PRIMARY KEY,name TEXT,deleted_at TIMESTAMPTZ);INSERT INTO ai_pitfall_configs(id,name) VALUES(9,'pitfall');
 CREATE TABLE fault_combinations(id BIGSERIAL PRIMARY KEY,first_defect_id BIGINT,second_defect_id BIGINT);INSERT INTO fault_combinations(id,first_defect_id,second_defect_id) VALUES(4,11,17);`)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../migrations/000005_configuration_ownership.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	var id int64
	var provenance string
	if e = pool.QueryRow(ctx, "SELECT id,provenance::text FROM content_defects WHERE id=11").Scan(&id, &provenance); e != nil || id != 11 || !strings.Contains(provenance, "incident") {
		t.Fatalf("history lost: %d %s %v", id, provenance, e)
	}
	if e = pool.QueryRow(ctx, "INSERT INTO content_defects(name) VALUES('new') RETURNING id").Scan(&id); e != nil || id != 18 {
		t.Fatalf("owned sequence: %d %v", id, e)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM defects").Scan(&count); e != nil || count != 2 {
		t.Fatalf("legacy modified: %d %v", count, e)
	}
	if e = pool.QueryRow(ctx, "SELECT id FROM content_ai_pitfall_configs WHERE id=9").Scan(&id); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT id FROM content_fault_combinations WHERE id=4").Scan(&id); e != nil {
		t.Fatal(e)
	}
}
