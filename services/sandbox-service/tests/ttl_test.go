package tests

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// fakeClock 可控时钟。
type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

// TestIsExpiredRules 验证活跃度租期（lease-with-renewal）的到期判定：
// 以最近活动时刻起算租期、结束后走短宽限期、再长的活跃班次由绝对上限兜底。
func TestIsExpiredRules(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	lease := model.ActivityLease

	cases := []struct {
		name string
		sb   model.Sandbox
		want bool
	}{
		{
			name: "刚操作过不回收",
			sb: model.Sandbox{
				Status: model.StatusRunning, CreatedAt: now.Add(-3 * time.Hour),
				LastActivityAt: now.Add(-5 * time.Minute),
			},
			want: false,
		},
		{
			name: "空闲超过活跃度租期回收",
			sb: model.Sandbox{
				Status: model.StatusRunning, CreatedAt: now.Add(-50 * time.Minute),
				LastActivityAt: now.Add(-(lease + time.Minute)),
			},
			want: true,
		},
		{
			name: "从无活动按创建时间起算",
			sb:   model.Sandbox{Status: model.StatusRunning, CreatedAt: now.Add(-lease - time.Minute)},
			want: true,
		},
		{
			name: "恰好达到租期即回收",
			sb:   model.Sandbox{Status: model.StatusRunning, CreatedAt: now.Add(-lease)},
			want: true,
		},
		{
			name: "一直活跃也受绝对上限兜底（防资源永驻）",
			sb: model.Sandbox{
				Status: model.StatusRunning, CreatedAt: now.Add(-model.MaxSandboxLifetime - time.Minute),
				LastActivityAt: now.Add(-time.Minute),
			},
			want: true,
		},
		{
			name: "单行租期覆盖（长租期内不回收）",
			sb: model.Sandbox{
				Status: model.StatusRunning, Lease: time.Hour,
				CreatedAt: now.Add(-45 * time.Minute),
			},
			want: false,
		},
		{
			name: "实训结束且过宽限期回收",
			sb: model.Sandbox{
				Status:      model.StatusRunning,
				CreatedAt:   now.Add(-20 * time.Minute),
				CompletedAt: now.Add(-model.ReleaseGraceAfterComplete - time.Minute),
			},
			want: true,
		},
		{
			name: "实训刚结束在宽限期内保留（留给复盘生成）",
			sb: model.Sandbox{
				Status:      model.StatusRunning,
				CreatedAt:   now.Add(-20 * time.Minute),
				CompletedAt: now.Add(-2 * time.Minute),
			},
			want: false,
		},
		{
			name: "已释放不再处理",
			sb:   model.Sandbox{Status: model.StatusReleased, CreatedAt: now.Add(-24 * time.Hour)},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.sb.IsExpired(now); got != c.want {
				t.Errorf("IsExpired=%v, want %v", got, c.want)
			}
		})
	}
}

// TestReapExpiredReleasesOnlyExpired 验证回收时只删过期沙箱、保留未过期的，并回写状态。
func TestReapExpiredReleasesOnlyExpired(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	expired := model.Sandbox{
		SessionID: 101, UserID: 201, Namespace: "sandbox-101",
		Status: model.StatusRunning, CreatedAt: now.Add(-3 * time.Hour),
	}
	active := model.Sandbox{
		SessionID: 102, UserID: 202, Namespace: "sandbox-102",
		Status: model.StatusRunning, CreatedAt: now.Add(-10 * time.Minute),
	}

	// 集群里两个沙箱 namespace 都在
	cs := fake.NewSimpleClientset(
		service.BuildNamespace("101", "201"),
		service.BuildNamespace("102", "202"),
	)
	client := service.NewK8sClient(cs)
	store := &fakeStore{all: []model.Sandbox{expired, active}}
	rec := newTestReconciler(client, store)

	reaped, err := rec.ReapExpired(context.Background(), now)
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if len(reaped) != 1 || reaped[0] != 101 {
		t.Fatalf("应只回收101，实际: %v", reaped)
	}

	// 过期的 namespace 被删
	if _, err := cs.CoreV1().Namespaces().Get(context.Background(), "sandbox-101", metav1.GetOptions{}); err == nil {
		t.Error("过期沙箱 namespace 未删除")
	}
	// 未过期的保留
	if _, err := cs.CoreV1().Namespaces().Get(context.Background(), "sandbox-102", metav1.GetOptions{}); err != nil {
		t.Error("未过期沙箱被误删")
	}
	// 状态已回写为 released
	if store.status[101] != model.StatusReleased {
		t.Errorf("过期沙箱状态未回写 released，实际: %s", store.status[101])
	}
	if _, marked := store.status[102]; marked {
		t.Error("未过期沙箱不应被标记")
	}
}

// TestTTLScannerClock 验证时钟可注入（不真正起 ticker，直接验证判定随时间变化），
// 并验证"活动即续租"：同一沙箱在续租后重新回到未过期，停手后才判过期。
func TestTTLScannerClock(t *testing.T) {
	created := time.Now()
	sb := model.Sandbox{Status: model.StatusRunning, CreatedAt: created}
	if sb.IsExpired(created.Add(10 * time.Minute)) {
		t.Error("10分钟时不应过期")
	}
	if !sb.IsExpired(created.Add(2*time.Hour + time.Minute)) {
		t.Error("超过 2 小时无任何活动应过期")
	}

	sb.LastActivityAt = created.Add(2 * time.Hour)
	if sb.IsExpired(created.Add(2*time.Hour + 20*time.Minute)) {
		t.Error("刚续租 20 分钟仍在租期内，不应过期")
	}
	if !sb.IsExpired(created.Add(2*time.Hour + model.ActivityLease + time.Minute)) {
		t.Error("停手超过活跃度租期应过期")
	}
}
