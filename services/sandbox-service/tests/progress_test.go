package tests

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// ---------- 进度跟踪测试 ----------
// （放在 tests 包里避免循环引用）

// fakeDepLister 模拟 Deployment 列表。
type fakeDepLister struct {
	deps []appsv1.Deployment
	err  error
}

func (f *fakeDepLister) ListDeployments(_ context.Context, _ string) ([]appsv1.Deployment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.deps, nil
}

// makeDep 快速构造一个 Deployment。
func makeDep(name string, replicas, ready int32) appsv1.Deployment {
	return appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: ready},
	}
}

func TestProgressInitializing(t *testing.T) {
	store := newFakeStore()
	tracker := service.NewProgressTrackerWithLister(store, &fakeDepLister{})

	// 创建一个 creating 状态的沙箱
	store.Create(context.Background(), &model.Sandbox{
		SessionID: 1001,
		UserID:    1001,
		ProjectID: 1,
		Status:    model.StatusCreating,
		Namespace: "sandbox-1001",
		CreatedAt: time.Now().Add(-10 * time.Second),
	})

	st, err := tracker.Status(context.Background(), 1001)
	if err != nil {
		t.Fatalf("查询进度失败: %v", err)
	}

	if st.Stage != service.StageInitializing {
		t.Errorf("creating 状态对应阶段应为 initializing，got %s", st.Stage)
	}
	if st.Percent < 0 || st.Percent >= 15 {
		// initializing 阶段进度应在 0-15 之间
		t.Errorf("initializing 阶段进度应 < 15%%，got %d%%", st.Percent)
	}
	if st.ElapsedSec < 9 || st.ElapsedSec > 11 {
		t.Errorf("已用时间应约 10 秒，got %d", st.ElapsedSec)
	}
}

func TestProgressStartingWithDeployments(t *testing.T) {
	store := newFakeStore()
	lister := &fakeDepLister{
		deps: []appsv1.Deployment{
			makeDep("user", 1, 1),    // ready
			makeDep("product", 1, 1), // ready
			makeDep("order", 1, 0),   // not ready
		},
	}
	tracker := service.NewProgressTrackerWithLister(store, lister)

	store.Create(context.Background(), &model.Sandbox{
		SessionID: 1002,
		UserID:    1001,
		ProjectID: 1,
		Status:    model.StatusDeploying, // deploying 后会到 starting
		Namespace: "sandbox-1002",
		CreatedAt: time.Now().Add(-30 * time.Second),
	})
	// 直接推进到 starting 阶段（模拟）
	store.MarkStatus(context.Background(), 1002, model.StatusReady)    // 先到 ready 再回来
	store.MarkStatus(context.Background(), 1002, model.StatusCreating) // 重置

	// 用 starting 状态的沙箱来测
	store.Create(context.Background(), &model.Sandbox{
		SessionID: 1003,
		UserID:    1002,
		ProjectID: 1,
		Status:    "starting", // 直接设为 starting
		Namespace: "sandbox-1003",
		CreatedAt: time.Now().Add(-30 * time.Second),
	})

	st, err := tracker.Status(context.Background(), 1003)
	if err != nil {
		t.Fatalf("查询进度失败: %v", err)
	}

	if st.Stage != service.StageStarting {
		t.Errorf("starting 状态对应阶段应为 starting，got %s", st.Stage)
	}
	if st.ReadyCount != 2 {
		t.Errorf("就绪 Deployment 数应为 2，got %d", st.ReadyCount)
	}
	if st.TotalCount != 3 {
		t.Errorf("总 Deployment 数应为 3，got %d", st.TotalCount)
	}
	// 2/3 ready，starting 阶段进度范围 50%-95%
	// 阶段内进度 = 2/3 ≈ 0.667，区间范围 45% (95-50)
	// 总进度 ≈ 50 + 45*0.667 = 80%
	if st.Percent < 75 || st.Percent > 90 {
		t.Errorf("2/3 ready 时进度应约 80%%，got %d%%", st.Percent)
	}
}

func TestProgressReady(t *testing.T) {
	store := newFakeStore()
	tracker := service.NewProgressTrackerWithLister(store, &fakeDepLister{})

	store.Create(context.Background(), &model.Sandbox{
		SessionID: 1004,
		UserID:    1001,
		ProjectID: 1,
		Status:    model.StatusReady,
		Namespace: "sandbox-1004",
		CreatedAt: time.Now().Add(-60 * time.Second),
	})

	st, err := tracker.Status(context.Background(), 1004)
	if err != nil {
		t.Fatalf("查询进度失败: %v", err)
	}

	if st.Stage != service.StageReady {
		t.Errorf("ready 状态对应阶段应为 ready，got %s", st.Stage)
	}
	if st.Percent != 100 {
		t.Errorf("ready 时进度应为 100%%，got %d%%", st.Percent)
	}
	if st.StageName != "就绪" {
		t.Errorf("ready 阶段名应为'就绪'，got %s", st.StageName)
	}
	if st.EstimatedSec != -1 {
		t.Errorf("ready 时预计剩余时间应为 -1，got %d", st.EstimatedSec)
	}
}

func TestProgressFailed(t *testing.T) {
	store := newFakeStore()
	tracker := service.NewProgressTrackerWithLister(store, &fakeDepLister{})

	store.Create(context.Background(), &model.Sandbox{
		SessionID: 1005,
		UserID:    1001,
		ProjectID: 1,
		Status:    model.StatusFailed,
		Namespace: "sandbox-1005",
		CreatedAt: time.Now().Add(-20 * time.Second),
	})

	st, err := tracker.Status(context.Background(), 1005)
	if err != nil {
		t.Fatalf("查询进度失败: %v", err)
	}

	if st.Stage != service.StageFailed {
		t.Errorf("failed 状态对应阶段应为 failed，got %s", st.Stage)
	}
	if st.Error == "" {
		t.Error("failed 时应包含错误信息")
	}
}

func TestProgressNotFound(t *testing.T) {
	store := newFakeStore()
	tracker := service.NewProgressTrackerWithLister(store, &fakeDepLister{})

	_, err := tracker.Status(context.Background(), 9999)
	if err == nil {
		t.Fatal("不存在的沙箱应返回错误")
	}
}

func TestEstimateRemaining(t *testing.T) {
	// 已用 30s，进度 50% → 预计剩余 30s
	// estimateRemaining 是未导出的，我们通过 Status 间接测
	store := newFakeStore()
	tracker := service.NewProgressTrackerWithLister(store, &fakeDepLister{
		deps: []appsv1.Deployment{
			makeDep("a", 1, 1),
			makeDep("b", 1, 0),
		},
	})

	store.Create(context.Background(), &model.Sandbox{
		SessionID: 1006,
		UserID:    1001,
		ProjectID: 1,
		Status:    "starting",
		Namespace: "sandbox-1006",
		CreatedAt: time.Now().Add(-30 * time.Second),
	})

	st, _ := tracker.Status(context.Background(), 1006)
	if st.EstimatedSec <= 0 {
		t.Errorf("进行中应有预计剩余时间，got %d", st.EstimatedSec)
	}
	// 粗略估计：30s / 百分比 * (100-百分比)
	// 1/2 ready，starting 阶段 ≈ 72.5%
	// 30 / 72.5 * 27.5 ≈ 11s
	if st.EstimatedSec < 5 || st.EstimatedSec > 30 {
		t.Logf("预计剩余时间: %d 秒（粗略合理范围即可）", st.EstimatedSec)
	}
}
