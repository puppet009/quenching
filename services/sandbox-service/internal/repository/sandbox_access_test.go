package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/sandbox-service/internal/model"
)

func TestPGWorkspaceAccessSerializesCompletion(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent; real PostgreSQL authorization locking not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := "access_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer root.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// These operations need only the sandbox aggregate and activity columns.
	// Ownership migration 000012 needs training-domain tables and belongs in a
	// cross-service migration fixture, not this isolated row-lock test schema.
	files := []string{
		filepath.Join("..", "..", "migrations", "000001_create_sandboxes.up.sql"),
		filepath.Join("..", "..", "migrations", "000005_add_sandbox_activity_lease.up.sql"),
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	store := NewPGStore(pool)
	sb := &model.Sandbox{SessionID: 71, UserID: 9, Namespace: "sandbox-71", Status: model.StatusCreating}
	if err := store.Create(ctx, sb); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	ensured, completed := make(chan error, 1), make(chan error, 1)
	go func() {
		ensured <- store.WithWorkspaceAccess(ctx, 71, func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() { completed <- store.MarkCompleted(ctx, 71, time.Now(), "completed") }()
	select {
	case err := <-completed:
		t.Fatal("completion acknowledged while authorization was still being created", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-ensured; err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	called := false
	err = store.WithWorkspaceAccess(ctx, 71, func(context.Context) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("post-completion allocation reopened workspace", err, called)
	}
}
