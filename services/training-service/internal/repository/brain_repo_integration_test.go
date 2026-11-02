//go:build integration

package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"os"
	"sync"
	"testing"
	"time"
)

func brainTestDatabase(t *testing.T) (context.Context, *BrainRepo, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("BRAIN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "brain_test_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, dropErr := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		if dropErr != nil {
			t.Error(dropErr)
		}
		admin.Close(ctx)
	})
	_, err = pool.Exec(ctx, `CREATE TABLE training_sessions(id BIGINT PRIMARY KEY,status TEXT,deleted_at TIMESTAMPTZ,template_slug TEXT); INSERT INTO training_sessions VALUES(1,'running',NULL,'mall');`)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile("../../migrations/000020_create_brain_state.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	repo := NewBrainRepo(pool)
	if err = repo.Ensure(ctx, 1); err != nil {
		t.Fatal(err)
	}
	return ctx, repo, pool
}
func TestBrainPGLeaseDuplicateAndRecovery(t *testing.T) {
	ctx, r, pool := brainTestDatabase(t)
	var wg sync.WaitGroup
	leases := make(chan *model.BrainLease, 2)
	failures := make(chan error, 2)
	for _, owner := range []string{"one", "two"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			l, e := r.Acquire(ctx, 1, owner, 30*time.Second)
			if e != nil {
				failures <- e
			} else {
				leases <- l
			}
		}(owner)
	}
	wg.Wait()
	if len(leases) != 1 || len(failures) != 1 {
		t.Fatal("exactly one writer required")
	}
	lease := <-leases
	event := model.BrainEvent{SessionID: 1, Source: "runner", EventID: "command1", Kind: "business_result"}
	fresh, err := r.AppendEvent(ctx, event)
	if err != nil || !fresh {
		t.Fatalf("first event: %v", err)
	}
	fresh, err = r.AppendEvent(ctx, event)
	if err != nil || fresh {
		t.Fatalf("duplicate: %v", err)
	}
	d := model.BrainDecision{DecisionID: "d1", ObservationVersion: 1, Action: "run_user_behavior", ParametersJSON: "{}", ActualModel: "qwen3.8-flash", PromptVersion: "director:v1"}
	cmd := model.BrainCommand{CommandID: "c1", SessionID: 1, DecisionID: "d1", ObservationVersion: 1, Domain: "sandbox", Action: d.Action, ParametersJSON: "{}"}
	if err = r.CommitDecision(ctx, *lease, 0, d, []model.BrainCommand{cmd}); err == nil {
		t.Fatal("stale decision accepted")
	}
	if err = r.CommitDecision(ctx, *lease, 1, d, []model.BrainCommand{cmd}); err != nil {
		t.Fatal(err)
	}
	restarted := NewBrainRepo(pool)
	pending, err := restarted.PendingCommands(ctx, 1)
	if err != nil || len(pending) != 1 || pending[0].CommandID != "c1" {
		t.Fatalf("recover pending: %v", err)
	}
	result := model.BrainToolResult{CommandID: "c1", Status: "unknown", ResultJSON: "{}"}
	if err = restarted.CompleteCommand(ctx, "c1", result); err != nil {
		t.Fatal(err)
	}
	pending, err = restarted.PendingCommands(ctx, 1)
	if err != nil || service.BrainRecoveryAction(pending[0]) != "query_status" {
		t.Fatal("unknown command not queried")
	}
	if _, err = pool.Exec(ctx, `UPDATE training_sessions SET status='aborted' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	d.DecisionID = "late"
	d.ObservationVersion = 2
	if err = r.CommitDecision(ctx, *lease, 2, d, nil); err == nil {
		t.Fatal("late cancelled decision accepted")
	}
}
