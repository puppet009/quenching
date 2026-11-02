package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPGResourceQueueConcurrency(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent; real resource queue PostgreSQL not verified")
	}
	ctx := context.Background()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	schema := "resources_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, e = root.Exec(ctx, "CREATE SCHEMA "+schema)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
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
	files, _ := filepath.Glob("../../migrations/*.up.sql")
	for _, f := range files {
		raw, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatalf("%s: %v", f, e)
		}
	}
	store := NewPGStore(pool)
	sb := &model.Sandbox{SessionID: 1, UserID: 10, ProjectID: 0, Namespace: "sandbox-1", Status: model.StatusQueued, TemplateSlug: "mall", TemplateDigest: "digest", ResourcePlan: model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 1000}}}
	if e = store.CreateQueued(ctx, sb); e != nil {
		t.Fatal(e)
	}
	retry := *sb
	if e = store.CreateQueued(ctx, &retry); e != nil || retry.ID != sb.ID {
		t.Fatal(retry, e)
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		done <- store.WithAllocationLock(ctx, func(context.Context) error {
			calls.Add(1)
			close(entered)
			time.Sleep(150 * time.Millisecond)
			return nil
		})
	}()
	<-entered
	if e = store.WithAllocationLock(ctx, func(context.Context) error { calls.Add(1); return nil }); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil || calls.Load() != 1 {
		t.Fatal(e, calls.Load())
	}
	if e = store.MarkCompleted(ctx, 1, time.Now(), "completed"); e != nil {
		t.Fatal(e)
	}
	ok, e := store.Admit(ctx, 1)
	if e != nil || ok {
		t.Fatal("closed queue admitted", ok, e)
	}
	if e = store.MarkStatus(ctx, 1, model.StatusReleased); e != nil {
		t.Fatal(e)
	}
	retry.Status = model.StatusQueued
	retry.CreatedAt = time.Time{}
	retry.CompletedAt = time.Time{}
	if e = store.CreateQueued(ctx, &retry); e == nil {
		t.Fatal("released session inserted anew")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM sandboxes WHERE session_id=1`).Scan(&count); e != nil || count != 1 {
		t.Fatal(e, count)
	}
}

func TestQueuedReuseRejectsTerminalAndExpired(t *testing.T) {
	want := model.Sandbox{UserID: 2, SessionID: 1, TemplateSlug: "mall", TemplateDigest: "digest", Status: model.StatusQueued, CreatedAt: time.Now()}
	for _, status := range []string{model.StatusReleased, model.StatusFailed, model.StatusReleasing} {
		old := want
		old.Status = status
		if e := service.ValidateQueuedReuse(old, want, time.Now()); e == nil {
			t.Fatal(status)
		}
	}
	old := want
	old.CreatedAt = time.Now().Add(-time.Hour)
	if e := service.ValidateQueuedReuse(old, want, time.Now()); e == nil {
		t.Fatal("expired")
	}
}
