package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"os"
	"testing"
	"time"
)

func TestGrowthPGLeaseCASAndUniqueArtifacts(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("growth_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE reviews(id bigint PRIMARY KEY,user_id bigint,session_id bigint,project_id bigint,deleted_at timestamptz,generation_status text,updated_at timestamptz);INSERT INTO reviews VALUES(1,2,3,4,NULL,'pending',now())`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/000006_growth_review_jobs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	repo := NewGrowthJobRepo(pool)
	snap := rc.Snapshot{SessionID: 3, UserID: 2, ProjectID: 4, Version: 1, Hash: "frozen"}
	if err = repo.Ensure(ctx, 1, snap); err != nil {
		t.Fatal(err)
	}
	l, err := repo.Claim(ctx, 1, "first", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Claim(ctx, 1, "second", time.Minute); !errors.Is(err, model.ErrGrowthBusy) {
		t.Fatal("double lease", err)
	}
	job, err := repo.State(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	a := &rc.AnalysisArtifact{SchemaVersion: 1, Coverage: json.RawMessage(`{}`)}
	if err = repo.Commit(ctx, l, job.Version, job, a); err != nil {
		t.Fatal(err)
	}
	if err = repo.Commit(ctx, l, job.Version, job, a); !errors.Is(err, model.ErrGrowthLeaseLost) {
		t.Fatal("late inference committed", err)
	}
	l2, err := repo.Claim(ctx, 1, "next", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Artifacts(ctx, l2, 0)
	if err != nil || len(rows) != 1 {
		t.Fatal("duplicate artifact", len(rows), err)
	}
	if _, err = pool.Exec(ctx, `UPDATE growth_review_jobs SET lease_until=now()-interval '1 second' WHERE review_id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.State(ctx, l2); !errors.Is(err, model.ErrGrowthLeaseLost) {
		t.Fatal("expired lease accepted", err)
	}
}
