//go:build integration

package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLegacyClosurePGIncludesAcceptedFinishBeforeStart(t *testing.T) {
	ctx, _, pool := brainTestDatabase(t)
	_, err := pool.Exec(ctx, `ALTER TABLE training_sessions
 ADD COLUMN user_id BIGINT,ADD COLUMN project_id BIGINT,
 ADD COLUMN started_at TIMESTAMPTZ,ADD COLUMN ended_at TIMESTAMPTZ;
 INSERT INTO training_sessions(id,status,user_id,project_id,ended_at) VALUES
 (2,'aborted',9,2,now()),(3,'failed',9,2,NULL),(4,'completed',9,2,now());`)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := NewSessionRepo(pool, nil, nil, nil, nil).ListClosedLegacySessions(ctx, 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if !reflect.DeepEqual(ids, []int64{2, 4}) {
		t.Fatalf("accepted pre-start finish was lost or deployment-only failure was reviewed: %v", ids)
	}
}

func TestScoreSessionPGClaimKeepsPoolHeadroomAndSurvivesReplicas(t *testing.T) {
	ctx, _, original := brainTestDatabase(t)
	cfg := original.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	first, second := NewScoreRepo(pool, nil, nil), NewScoreRepo(pool, nil, nil)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	entered, proceed := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := first.WithScoreSessionLock(ctx, 7, func(ctx context.Context) error {
			var one int
			if err := pool.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil {
				return err // A retained pool slot would deadlock with MaxConns=1.
			}
			close(entered)
			select {
			case <-proceed:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		finished <- err
	}()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("scoring lock exhausted query pool headroom: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	called := false
	acquired, err := second.WithScoreSessionLock(ctx, 7, func(context.Context) error { called = true; return nil })
	close(proceed)
	if firstErr := <-finished; firstErr != nil {
		t.Fatal(firstErr)
	}
	if err != nil || acquired || called {
		t.Fatalf("another replica entered the session's score flow: acquired=%v called=%v err=%v", acquired, called, err)
	}
	acquired, err = second.WithScoreSessionLock(ctx, 7, func(context.Context) error { return nil })
	if err != nil || !acquired {
		t.Fatalf("finished scoring did not release the database claim: acquired=%v err=%v", acquired, err)
	}
}
