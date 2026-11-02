package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// newQuotaService 辅助构造 QuotaService。
func newQuotaService(store service.Store) *service.QuotaService {
	return service.NewQuotaService(store)
}

// TestCheckCreateQuota_NewUserPass 新用户（无活跃沙箱）应通过校验。
func TestCheckCreateQuota_NewUserPass(t *testing.T) {
	store := &fakeStore{}
	qs := newQuotaService(store)

	err := qs.CheckCreateQuota(context.Background(), 1001)
	if err != nil {
		t.Fatalf("新用户应通过配额校验，实际报错: %v", err)
	}
}

// TestCheckCreateQuota_Exceeded 用户已达上限应被拒绝，且错误消息含配额信息。
func TestCheckCreateQuota_Exceeded(t *testing.T) {
	store := &fakeStore{}
	qs := newQuotaService(store)
	ctx := context.Background()

	// 先创建一个活跃沙箱（达到上限 1）
	_ = store.Create(ctx, &model.Sandbox{
		SessionID: 1,
		UserID:    2001,
		ProjectID: 101,
		Status:    model.StatusRunning,
	})

	err := qs.CheckCreateQuota(ctx, 2001)
	if err == nil {
		t.Fatal("达到配额上限应被拒绝，实际通过了")
	}

	// 错误消息应包含配额信息
	msg := err.Error()
	if !strings.Contains(msg, "配额超限") {
		t.Errorf("错误消息应包含'配额超限'，实际: %s", msg)
	}
}

// TestCheckCreateQuota_ReleasedNotCounted 已释放的沙箱不计入配额。
func TestCheckCreateQuota_ReleasedNotCounted(t *testing.T) {
	store := &fakeStore{}
	qs := newQuotaService(store)
	ctx := context.Background()

	// 创建一个已释放的沙箱
	_ = store.Create(ctx, &model.Sandbox{
		SessionID: 2,
		UserID:    2002,
		ProjectID: 101,
		Status:    model.StatusReleased,
	})

	err := qs.CheckCreateQuota(ctx, 2002)
	if err != nil {
		t.Fatalf("已释放的沙箱不应计入配额，实际报错: %v", err)
	}
}

// TestCheckCreateQuota_DifferentUsers 不同用户配额独立。
func TestCheckCreateQuota_DifferentUsers(t *testing.T) {
	store := &fakeStore{}
	qs := newQuotaService(store)
	ctx := context.Background()

	// 用户 A 有 1 个活跃沙箱（已满）
	_ = store.Create(ctx, &model.Sandbox{
		SessionID: 3,
		UserID:    3001,
		Status:    model.StatusRunning,
	})

	// 用户 A 再创建应被拒
	if err := qs.CheckCreateQuota(ctx, 3001); err == nil {
		t.Error("用户A达到上限应被拒绝")
	}

	// 用户 B 不受影响，应通过
	if err := qs.CheckCreateQuota(ctx, 3002); err != nil {
		t.Errorf("用户B不应受用户A配额影响，实际报错: %v", err)
	}
}

// TestCheckCreateQuota_FailedStatusCounted failed 状态的沙箱仍算占用（资源未释放）。
func TestCheckCreateQuota_FailedStatusCounted(t *testing.T) {
	store := &fakeStore{}
	qs := newQuotaService(store)
	ctx := context.Background()

	_ = store.Create(ctx, &model.Sandbox{
		SessionID: 4,
		UserID:    4001,
		Status:    model.StatusFailed,
	})

	// failed 状态仍占用资源，计入配额
	err := qs.CheckCreateQuota(ctx, 4001)
	if err == nil {
		t.Fatal("failed 状态的沙箱仍应计入配额（资源未释放）")
	}
}
