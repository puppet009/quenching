package repository

import (
	"context"
	"fmt"
	"time"
)

// WithScoreSessionLock serializes the whole score pipeline across replicas,
// including the non-idempotent collaborative profile update in user-service.
func (r *ScoreRepo) WithScoreSessionLock(ctx context.Context, sid int64, run func(context.Context) error) (bool, error) {
	pooled, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	// Detach the lock connection before the callback needs ordinary pool queries.
	// Keeping an acquired slot here would deadlock when scorers fill the pool.
	conn := pooled.Hijack()
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx) // Closing also releases a lock if unlock failed.
	}()
	key := fmt.Sprintf("training-session-score:%d", sid)
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&acquired); err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key)
	}()
	return true, run(ctx)
}
