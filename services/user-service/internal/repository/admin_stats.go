package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ReadAdminStats(ctx context.Context, pool *pgxpool.Pool) (map[string]interface{}, error) {
	var total int64
	err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE deleted_at IS NULL").Scan(&total)
	return map[string]interface{}{"totalUsers": total}, err
}
