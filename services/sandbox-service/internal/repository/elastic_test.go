package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPGElasticHoldsSurviveRestartAndSerializeMutations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent; real elastic PostgreSQL persistence/locking not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	schema := "elastic_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = root.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if _, err := root.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
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
	raw, e := os.ReadFile("../../migrations/000011_elastic_resources.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	a, b := NewPGStore(pool), NewPGStore(pool)
	for _, id := range []string{"first", "second", "first"} {
		if e = a.AcquireScalingHold(ctx, model.ScalingHold{Namespace: "sandbox-1", ID: id, Reason: "cpu"}); e != nil {
			t.Fatal(e)
		}
	}
	state, e := b.ElasticState(ctx)
	if e != nil || len(state.Holds) != 2 {
		t.Fatal("holds lost after service reconstruction", state, e)
	}
	if e = b.ReleaseScalingHold(ctx, "sandbox-1", "first"); e != nil {
		t.Fatal(e)
	}
	state, e = a.ElasticState(ctx)
	if e != nil || len(state.Holds) != 1 || state.Holds[0].ID != "second" {
		t.Fatal(state, e)
	}
	p := model.ElasticPolicy{Namespace: "platform", Name: "content-service", Enabled: true}
	if e = a.InsertElasticPolicy(ctx, p); e != nil {
		t.Fatal(e)
	}
	state, e = b.ElasticState(ctx)
	if e != nil {
		t.Fatal(e)
	}
	p = state.Policies[0]
	stale := p
	if e = a.SaveElasticPolicy(ctx, &p, nil); e != nil {
		t.Fatal(e)
	}
	if e = b.SaveElasticPolicy(ctx, &stale, nil); !errors.Is(e, service.ErrResourceConflict) {
		t.Fatal("stale policy overwrote state", e)
	}
	entered, finish, second := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 2)
	go func() {
		done <- a.WithElasticLock(ctx, true, func(context.Context) error {
			close(entered)
			select {
			case <-finish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	<-entered
	go func() {
		done <- b.WithElasticLock(ctx, true, func(context.Context) error { close(second); return nil })
	}()
	select {
	case <-second:
		t.Fatal("freeze and scale lock overlapped")
	case <-time.After(150 * time.Millisecond):
	}
	close(finish)
	for i := 0; i < 2; i++ {
		if e = <-done; e != nil {
			t.Fatal(e)
		}
	}
}
