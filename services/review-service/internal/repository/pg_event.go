package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/review-service/internal/model"
)

// PGEventRepo 基于 PostgreSQL events 表的埋点仓储（生产实现）。
//
// 唯一事件（register/first_start/first_complete）由迁移中的部分唯一索引
// idx_events_unique_user_event 兜底，写入使用 ON CONFLICT DO NOTHING，
// RowsAffected==0 即视为重复上报。调用方无需自己先查后写。
type PGEventRepo struct {
	pool *pgxpool.Pool
}

// NewPGEventRepo 创建 PG 事件仓储。
func NewPGEventRepo(pool *pgxpool.Pool) *PGEventRepo {
	return &PGEventRepo{pool: pool}
}

// Track 记录事件；唯一事件重复时返回 (false, nil)。
func (r *PGEventRepo) Track(ctx context.Context, event *model.Event) (bool, error) {
	if event.EventType == "" {
		return false, fmt.Errorf("事件类型不能为空")
	}

	now := time.Now()
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = now
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}

	// nil properties 归一到空对象，避免写入 JSONB 'null' 导致属性统计取不到值。
	props := event.Properties
	if props == nil {
		props = map[string]interface{}{}
	}
	raw, err := json.Marshal(props)
	if err != nil {
		return false, fmt.Errorf("序列化事件属性失败: %w", err)
	}

	// 无冲突目标的 ON CONFLICT 能覆盖部分唯一索引，非唯一事件不会触发冲突。
	tag, err := r.pool.Exec(ctx, `
INSERT INTO events (event_type, user_id, session_id, project_id, properties,
                    client_ip, user_agent, platform, occurred_at, received_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT DO NOTHING`,
		event.EventType,
		event.UserID,
		nullableID(event.SessionID),
		nullableID(event.ProjectID),
		raw,
		nullableString(event.ClientIP),
		nullableString(event.UserAgent),
		nullableString(event.Platform),
		event.OccurredAt,
		event.ReceivedAt,
	)
	if err != nil {
		return false, fmt.Errorf("写入事件失败: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// HasUserEvent 检查用户是否已触发过某事件。
func (r *PGEventRepo) HasUserEvent(ctx context.Context, userID int64, eventType string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM events WHERE user_id = $1 AND event_type = $2)`,
		userID, eventType).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("查询用户事件失败: %w", err)
	}
	return exists, nil
}

// CountByEventType 统计事件触发次数；unique=true 时按用户去重。
func (r *PGEventRepo) CountByEventType(
	ctx context.Context, eventType string, unique bool, start, end time.Time,
) (int, error) {
	if eventType == "" {
		return 0, fmt.Errorf("事件类型不能为空")
	}

	countExpr := "COUNT(*)"
	if unique {
		countExpr = "COUNT(DISTINCT user_id)"
	}

	var count int64
	err := r.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT %s FROM events
WHERE event_type = $1
  AND ($2::timestamptz IS NULL OR occurred_at >= $2)
  AND ($3::timestamptz IS NULL OR occurred_at <= $3)`, countExpr),
		eventType, nullableTime(start), nullableTime(end)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计事件失败: %w", err)
	}
	return int(count), nil
}

// GetUniqueUsers 返回时间范围内触发过该事件的用户 ID 集合。
func (r *PGEventRepo) GetUniqueUsers(
	ctx context.Context, eventType string, start, end time.Time,
) (map[int64]bool, error) {
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT user_id FROM events
WHERE event_type = $1 AND user_id > 0
  AND ($2::timestamptz IS NULL OR occurred_at >= $2)
  AND ($3::timestamptz IS NULL OR occurred_at <= $3)`,
		eventType, nullableTime(start), nullableTime(end))
	if err != nil {
		return nil, fmt.Errorf("查询事件用户失败: %w", err)
	}
	defer rows.Close()

	users := make(map[int64]bool)
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("读取事件用户失败: %w", err)
		}
		users[uid] = true
	}
	return users, rows.Err()
}

// ListEvents 分页查询事件列表（最新在前）。
func (r *PGEventRepo) ListEvents(
	ctx context.Context, userID int64, eventType string, page, pageSize int,
) ([]model.Event, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM events
WHERE ($1 = 0 OR user_id = $1) AND ($2 = '' OR event_type = $2)`,
		userID, eventType).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计事件列表失败: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
SELECT id, event_type, user_id, COALESCE(session_id, 0), COALESCE(project_id, 0),
       properties, COALESCE(client_ip, ''), COALESCE(user_agent, ''),
       COALESCE(platform, ''), occurred_at, received_at
FROM events
WHERE ($1 = 0 OR user_id = $1) AND ($2 = '' OR event_type = $2)
ORDER BY occurred_at DESC, id DESC
LIMIT $3 OFFSET $4`,
		userID, eventType, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("查询事件列表失败: %w", err)
	}
	defer rows.Close()

	events := make([]model.Event, 0, pageSize)
	for rows.Next() {
		var e model.Event
		var rawProps []byte
		if err := rows.Scan(&e.ID, &e.EventType, &e.UserID, &e.SessionID, &e.ProjectID,
			&rawProps, &e.ClientIP, &e.UserAgent, &e.Platform, &e.OccurredAt, &e.ReceivedAt); err != nil {
			return nil, 0, fmt.Errorf("读取事件失败: %w", err)
		}
		if len(rawProps) > 0 {
			if err := json.Unmarshal(rawProps, &e.Properties); err != nil {
				return nil, 0, fmt.Errorf("解析事件属性失败: %w", err)
			}
		}
		events = append(events, e)
	}
	return events, int(total), rows.Err()
}

// GetAverageNumericProperty 获取指定数值属性的平均值。
// 仅统计 JSONB 中类型为 number 的属性，避免字符串被 ::numeric 转换报错。
func (r *PGEventRepo) GetAverageNumericProperty(
	ctx context.Context, eventType string, propertyKey string, start, end time.Time,
) (float64, error) {
	var avg float64
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(AVG((properties ->> $2)::numeric), 0)
FROM events
WHERE event_type = $1
  AND jsonb_typeof(properties -> $2) = 'number'
  AND ($3::timestamptz IS NULL OR occurred_at >= $3)
  AND ($4::timestamptz IS NULL OR occurred_at <= $4)`,
		eventType, propertyKey, nullableTime(start), nullableTime(end)).Scan(&avg)
	if err != nil {
		return 0, fmt.Errorf("统计事件属性均值失败: %w", err)
	}
	return avg, nil
}

// CountByPropertyValue 按属性数值比较统计事件数。
// compare 支持 eq/ne/gt/gte/lt/lte，未知运算符返回 0。
func (r *PGEventRepo) CountByPropertyValue(
	ctx context.Context, eventType string, propertyKey string, compare string,
	value float64, start, end time.Time,
) (int, error) {
	op, ok := compareOperators[compare]
	if !ok {
		return 0, nil
	}

	var count int64
	err := r.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT COUNT(*) FROM events
WHERE event_type = $1
  AND jsonb_typeof(properties -> $2) = 'number'
  AND (properties ->> $2)::numeric %s $3
  AND ($4::timestamptz IS NULL OR occurred_at >= $4)
  AND ($5::timestamptz IS NULL OR occurred_at <= $5)`, op),
		eventType, propertyKey, value, nullableTime(start), nullableTime(end)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("按属性统计事件失败: %w", err)
	}
	return int(count), nil
}

// compareOperators 属性比较运算符白名单（防止 SQL 注入）。
var compareOperators = map[string]string{
	"eq":  "=",
	"ne":  "<>",
	"gt":  ">",
	"gte": ">=",
	"lt":  "<",
	"lte": "<=",
}

// nullableTime 零值时间返回 nil，交给 SQL 的 $n IS NULL 分支。
func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// nullableID 0 值视为未提供（DB 中为 NULL）。
func nullableID(id int64) interface{} {
	if id == 0 {
		return nil
	}
	return id
}

// nullableString 空串视为未提供。
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
