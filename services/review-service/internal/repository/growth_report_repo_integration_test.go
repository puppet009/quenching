package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGrowthCompletionPreservesExistingScores(t *testing.T) {
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
	schema := fmt.Sprintf("growth_report_%d", time.Now().UnixNano())
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
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		sql, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(sql)); e != nil {
			t.Fatal(file, e)
		}
	}
	reviews := NewReviewRepo(pool)
	rv := &model.Review{SessionID: 1, UserID: 2, ProjectID: 0, ReviewLevel: "method", GenerationStatus: "pending"}
	snap := rc.Snapshot{SessionID: 1, UserID: 2, Version: 1, Hash: "frozen"}
	if err = reviews.CreateGrowth(ctx, rv, snap); err != nil {
		t.Fatal(err)
	}
	if _, err = reviews.GetByID(ctx, rv.ID); err != nil {
		t.Fatal("new pending review must be readable", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE reviews SET tech_score=82,collab_score=71 WHERE id=$1`, rv.ID); err != nil {
		t.Fatal(err)
	}
	jobs := NewGrowthJobRepo(pool)
	l, err := jobs.Claim(ctx, rv.ID, "prepare", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.State(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	job.Phase = "finalize"
	job.ActualModel = "qwen3.8-flash"
	job.PromptVersion = "growth:v1"
	job.Report = &rc.GrowthReport{SchemaVersion: 1, Overview: json.RawMessage(`{}`), Ability: json.RawMessage(`{"overall_level":"unknown"}`)}
	if err = jobs.Commit(ctx, l, job.Version, job, nil); err != nil {
		t.Fatal(err)
	}
	l, err = jobs.Claim(ctx, rv.ID, "finish", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	job, err = jobs.State(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	var sections []model.ReviewSection
	for i := 0; i < 6; i++ {
		sections = append(sections, model.ReviewSection{ReviewID: rv.ID, SectionType: fmt.Sprint(i), SectionLevel: "method", Title: "chapter", Content: "cited", SortOrder: int16(i)})
	}
	if err = reviews.CompleteGrowth(ctx, l, job, sections, *job.Report); err != nil {
		t.Fatal(err)
	}
	after, err := reviews.GetByID(ctx, rv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.TechScore != 82 || after.CollabScore != 71 || after.GenerationStatus != "completed" {
		t.Fatal("existing score overwritten", after)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_sections WHERE review_id=$1`, rv.ID).Scan(&count); err != nil || count != 6 {
		t.Fatal("atomic sections", count, err)
	}
	if err = reviews.CompleteGrowth(ctx, l, job, sections, *job.Report); err == nil {
		t.Fatal("late completion accepted")
	}
}
