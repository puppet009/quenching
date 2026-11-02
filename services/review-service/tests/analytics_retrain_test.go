package tests

import (
	"context"
	"testing"
	"time"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
)

// TestTrack_FirstCompleteDerivesRetrain 验证：
// 生产端统一上报 first_complete，首次成功记录，重复上报自动补记 retrain。
func TestTrack_FirstCompleteDerivesRetrain(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()
	const uid int64 = 9001

	retrainCount := func() int {
		count, err := repo.CountByEventType(ctx, model.EventRetrain, false, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("统计 retrain 失败: %v", err)
		}
		return count
	}

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventFirstComplete,
		UserID:    uid,
		SessionID: 1,
	})
	if err != nil {
		t.Fatalf("首次完成上报失败: %v", err)
	}
	if !recorded {
		t.Fatal("首次完成应记录成功")
	}
	if got := retrainCount(); got != 0 {
		t.Fatalf("首次完成不应派生 retrain，实际 %d", got)
	}

	recorded, err = svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventFirstComplete,
		UserID:    uid,
		SessionID: 2,
	})
	if err != nil {
		t.Fatalf("重复完成上报失败: %v", err)
	}
	if recorded {
		t.Fatal("重复的 first_complete 应返回 recorded=false")
	}
	if got := retrainCount(); got != 1 {
		t.Fatalf("重复完成应派生 1 条 retrain，实际 %d", got)
	}

	// 第三次完成：first_complete 仍去重，retrain 每次都要记。
	recorded, err = svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventFirstComplete,
		UserID:    uid,
		SessionID: 3,
	})
	if err != nil || recorded {
		t.Fatalf("第三次完成应去重: recorded=%v err=%v", recorded, err)
	}
	if got := retrainCount(); got != 2 {
		t.Fatalf("两次重复完成应产生 2 条 retrain，实际 %d", got)
	}
}
