package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ReadAdminStats(ctx context.Context, pool *pgxpool.Pool) (map[string]interface{}, error) {
	var projects, defects int64
	err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM projects WHERE deleted_at IS NULL),(SELECT count(*) FROM content_defects WHERE deleted_at IS NULL)").Scan(&projects, &defects)
	return map[string]interface{}{"totalProjects": projects, "totalDefects": defects}, err
}
