package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func ReadAdminStats(ctx context.Context, pool *pgxpool.Pool) (map[string]interface{}, error) {
	var total, today int64
	var avg *float64
	err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE created_at>=date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),avg(final_score)::float8 FROM training_sessions WHERE deleted_at IS NULL`).Scan(&total, &today, &avg)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `WITH days AS (SELECT generate_series(date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai')-interval '6 days',date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai'),interval '1 day') AS day)
 SELECT day AT TIME ZONE 'Asia/Shanghai',count(t.id) FROM days LEFT JOIN training_sessions t
 ON t.created_at>=day AT TIME ZONE 'Asia/Shanghai' AND t.created_at<(day+interval '1 day') AT TIME ZONE 'Asia/Shanghai' AND t.deleted_at IS NULL GROUP BY day ORDER BY day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	trend := []map[string]interface{}{}
	for rows.Next() {
		var ts time.Time
		var value int64
		if err := rows.Scan(&ts, &value); err != nil {
			return nil, err
		}
		trend = append(trend, map[string]interface{}{"ts": ts.UnixMilli(), "value": value})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	feedback, err := NewFeedbackRepo(pool).Stats(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"totalTrainings": total, "todayTrainings": today, "avgScore": avg, "npsScore": feedback["nps_score"], "trend": trend}, nil
}
