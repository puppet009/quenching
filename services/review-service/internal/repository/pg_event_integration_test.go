package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/repository"
)

// TestPGEventRepo_Integration 需要真实 PG（含 000004 迁移）。
// 未设置 TEST_DATABASE_URL 时跳过，CI 默认不依赖数据库。
func TestPGEventRepo_Integration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过 PG 仓储集成测试")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	repo := repository.NewPGEventRepo(pool)

	// 用负 user_id 隔离测试数据，避免污染真实统计。
	const (
		userA int64 = -990001
		userB int64 = -990002
	)
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE user_id BETWEEN -999999 AND -1`)
	}
	cleanup()
	defer cleanup()

	now := time.Now()

	// 1) 普通事件：每次都能写入。
	for i := 0; i < 2; i++ {
		recorded, err := repo.Track(ctx, &model.Event{
			EventType:  model.EventTrainingComplete,
			UserID:     userA,
			SessionID:  int64(i + 1),
			Properties: map[string]interface{}{"duration_seconds": 120, "score": 4},
			OccurredAt: now,
		})
		if err != nil {
			t.Fatalf("写入普通事件失败: %v", err)
		}
		if !recorded {
			t.Fatalf("普通事件第 %d 次应记录成功", i+1)
		}
	}

	// 2) 唯一事件：第二次返回 recorded=false。
	if recorded, err := repo.Track(ctx, &model.Event{EventType: model.EventFirstComplete, UserID: userB, OccurredAt: now}); err != nil || !recorded {
		t.Fatalf("首次唯一事件应记录成功: recorded=%v err=%v", recorded, err)
	}
	if recorded, err := repo.Track(ctx, &model.Event{EventType: model.EventFirstComplete, UserID: userB, OccurredAt: now}); err != nil || recorded {
		t.Fatalf("重复唯一事件应去重: recorded=%v err=%v", recorded, err)
	}

	// 3) 次数 / 去重用户数。
	count, err := repo.CountByEventType(ctx, model.EventTrainingComplete, false, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("统计事件次数失败: %v", err)
	}
	if count < 2 {
		t.Fatalf("普通事件次数应 >= 2，实际 %d", count)
	}

	uniqueCount, err := repo.CountByEventType(ctx, model.EventFirstComplete, true, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("统计去重用户数失败: %v", err)
	}
	if uniqueCount < 1 {
		t.Fatalf("唯一事件去重用户数应 >= 1，实际 %d", uniqueCount)
	}

	// 4) 用户集合、列表。
	users, err := repo.GetUniqueUsers(ctx, model.EventTrainingComplete, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("查询事件用户失败: %v", err)
	}
	if !users[userA] {
		t.Fatalf("用户集合应包含 %d", userA)
	}

	events, total, err := repo.ListEvents(ctx, userA, model.EventTrainingComplete, 1, 10)
	if err != nil {
		t.Fatalf("查询事件列表失败: %v", err)
	}
	if total != 2 || len(events) != 2 {
		t.Fatalf("事件列表应返回 2 条，实际 total=%d len=%d", total, len(events))
	}
	if events[0].Properties == nil {
		t.Fatal("事件属性应被解析回 map")
	}

	// 5) 数值属性统计（JSONB number 类型保护）。
	avg, err := repo.GetAverageNumericProperty(ctx, model.EventTrainingComplete, "duration_seconds", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("统计属性均值失败: %v", err)
	}
	if avg != 120 {
		t.Fatalf("属性均值应为 120，实际 %v", avg)
	}

	byValue, err := repo.CountByPropertyValue(ctx, model.EventTrainingComplete, "score", "gte", 4, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("按属性统计失败: %v", err)
	}
	if byValue < 2 {
		t.Fatalf("score>=4 的次数应 >= 2，实际 %d", byValue)
	}
}
