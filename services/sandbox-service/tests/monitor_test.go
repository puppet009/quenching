// Package tests 沙箱监控后端单测。
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// ——— 辅助函数 ———

func newTestMonitorService() (*service.MonitorService, *service.InMemoryMonitorStore) {
	store := service.NewInMemoryMonitorStore()
	cfg := service.DefaultMonitorConfig()
	svc := service.NewMonitorService(store, cfg)
	return svc, store
}

func createTestSandbox(store *service.InMemoryMonitorStore, sessionID, userID, projectID int64, status string) {
	sb := &model.Sandbox{
		SessionID: sessionID,
		UserID:    userID,
		ProjectID: projectID,
		Namespace: model.NamespaceName(sessionID),
		Status:    status,
		CreatedAt: time.Now().Add(-30 * time.Minute),
		// 活跃度租期口径下测试沙箱视为"刚有过操作"，避免全部落在空闲超时边界上。
		LastActivityAt: time.Now(),
	}
	store.Create(context.Background(), sb)
}

// ——— 沙箱列表测试 ———

func TestListSandboxes_Pagination(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	// 创建 5 个沙箱
	for i := 1; i <= 5; i++ {
		createTestSandbox(store, int64(i), 100, 1, model.StatusRunning)
	}

	// 第1页，每页2条
	result, err := svc.ListSandboxes(ctx, model.SandboxQuery{
		Page:     1,
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("总数应为 5, got %d", result.Total)
	}
	if len(result.Items) != 2 {
		t.Errorf("第1页应有 2 条, got %d", len(result.Items))
	}

	// 第3页（最后一页）
	result, _ = svc.ListSandboxes(ctx, model.SandboxQuery{
		Page:     3,
		PageSize: 2,
	})
	if len(result.Items) != 1 {
		t.Errorf("第3页应有 1 条, got %d", len(result.Items))
	}
}

func TestListSandboxes_FilterByStatus(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)
	createTestSandbox(store, 2, 100, 1, model.StatusReady)
	createTestSandbox(store, 3, 100, 1, model.StatusReleased)
	createTestSandbox(store, 4, 100, 1, model.StatusFailed)

	// 只查 running
	result, _ := svc.ListSandboxes(ctx, model.SandboxQuery{
		Status:   model.StatusRunning,
		Page:     1,
		PageSize: 10,
	})
	if result.Total != 1 {
		t.Errorf("running 状态应有 1 个, got %d", result.Total)
	}

	// 查活跃的（is_active=true）
	active := true
	result, _ = svc.ListSandboxes(ctx, model.SandboxQuery{
		IsActive: &active,
		Page:     1,
		PageSize: 10,
	})
	if result.Total != 2 {
		t.Errorf("活跃沙箱应有 2 个（running+ready）, got %d", result.Total)
	}

	// 查已释放的（is_active=false）
	inactive := false
	result, _ = svc.ListSandboxes(ctx, model.SandboxQuery{
		IsActive: &inactive,
		Page:     1,
		PageSize: 10,
	})
	if result.Total != 2 {
		t.Errorf("非活跃沙箱应有 2 个（released+failed）, got %d", result.Total)
	}
}

func TestListSandboxes_FilterByUserAndProject(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)
	createTestSandbox(store, 2, 100, 2, model.StatusRunning)
	createTestSandbox(store, 3, 200, 1, model.StatusRunning)

	// 按用户筛选
	result, _ := svc.ListSandboxes(ctx, model.SandboxQuery{
		UserID:   100,
		Page:     1,
		PageSize: 10,
	})
	if result.Total != 2 {
		t.Errorf("用户 100 应有 2 个沙箱, got %d", result.Total)
	}

	// 按项目筛选
	result, _ = svc.ListSandboxes(ctx, model.SandboxQuery{
		ProjectID: 1,
		Page:      1,
		PageSize:  10,
	})
	if result.Total != 2 {
		t.Errorf("项目 1 应有 2 个沙箱, got %d", result.Total)
	}

	// 用户+项目组合
	result, _ = svc.ListSandboxes(ctx, model.SandboxQuery{
		UserID:    100,
		ProjectID: 1,
		Page:      1,
		PageSize:  10,
	})
	if result.Total != 1 {
		t.Errorf("用户 100 项目 1 应有 1 个沙箱, got %d", result.Total)
	}
}

// ——— 沙箱详情测试 ———

func TestGetSandboxDetail(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 42, 100, 1, model.StatusRunning)

	detail, err := svc.GetSandboxDetail(ctx, 42)
	if err != nil {
		t.Fatalf("获取详情失败: %v", err)
	}
	if detail.SessionID != 42 {
		t.Errorf("session_id 不匹配: %d", detail.SessionID)
	}
	if detail.RuntimeSeconds <= 0 {
		t.Error("运行时应大于 0")
	}
	if detail.RemainingSeconds <= 0 {
		t.Error("剩余时间应大于 0（刚有过操作，仍在活跃度租期内）")
	}
}

func TestGetSandboxDetail_WithMetrics(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 42, 100, 1, model.StatusRunning)

	// 上报一条指标
	svc.ReportMetrics(ctx, &model.ResourceMetrics{
		SessionID:     42,
		Namespace:     "sandbox-42",
		CPUCoresM:     500,
		CPUPercent:    25,
		MemoryBytes:   1024 * 1024 * 1024, // 1GB
		MemoryPercent: 25,
		PodCount:      3,
		PodRunning:    3,
	})

	detail, _ := svc.GetSandboxDetail(ctx, 42)
	if detail.Metrics == nil {
		t.Fatal("详情应包含最新指标")
	}
	if detail.Metrics.CPUPercent != 25 {
		t.Errorf("CPU 使用率应为 25%%, got %.1f", detail.Metrics.CPUPercent)
	}
}

// ——— 统计概览测试 ———

func TestGetSandboxStats(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)
	createTestSandbox(store, 2, 100, 1, model.StatusReady)
	createTestSandbox(store, 3, 100, 1, model.StatusReleased)

	stats, err := svc.GetSandboxStats(ctx)
	if err != nil {
		t.Fatalf("获取统计失败: %v", err)
	}
	if stats["total"].(int) != 3 {
		t.Errorf("总数应为 3, got %v", stats["total"])
	}
	if stats["active"].(int) != 2 {
		t.Errorf("活跃数应为 2, got %v", stats["active"])
	}
}

// ——— 资源监控测试 ———

func TestReportMetrics_AndLatest(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)

	metrics := &model.ResourceMetrics{
		SessionID:     1,
		Namespace:     "sandbox-1",
		CPUCoresM:     300,
		CPUPercent:    15,
		MemoryBytes:   512 * 1024 * 1024,
		MemoryPercent: 12.5,
		PodCount:      3,
		PodRunning:    3,
	}

	err := svc.ReportMetrics(ctx, metrics)
	if err != nil {
		t.Fatalf("上报指标失败: %v", err)
	}

	latest, err := svc.GetLatestMetrics(ctx, 1)
	if err != nil {
		t.Fatalf("获取最新指标失败: %v", err)
	}
	if latest == nil {
		t.Fatal("应返回最新指标")
	}
	if latest.CPUPercent != 15 {
		t.Errorf("CPU 使用率应为 15, got %.1f", latest.CPUPercent)
	}
}

func TestMetricsHistory(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)

	// 上报 5 条指标
	for i := 0; i < 5; i++ {
		svc.ReportMetrics(ctx, &model.ResourceMetrics{
			SessionID:     1,
			Namespace:     "sandbox-1",
			CPUPercent:    float64(10 + i*10),
			MemoryPercent: float64(20 + i*5),
			Timestamp:     time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	// 获取最近 3 条
	history, err := svc.GetMetricsHistory(ctx, 1, 3)
	if err != nil {
		t.Fatalf("获取历史失败: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("应返回 3 条, got %d", len(history))
	}
	// 最新的应该是最后一条（CPU 50%）
	if history[len(history)-1].CPUPercent != 50 {
		t.Errorf("最新一条 CPU 应为 50%%, got %.1f", history[len(history)-1].CPUPercent)
	}
}

// ——— 异常检测测试 ———

func TestDetectHighCPU(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)

	// CPU 95% → 严重告警
	err := svc.ReportMetrics(ctx, &model.ResourceMetrics{
		SessionID:     1,
		Namespace:     "sandbox-1",
		CPUPercent:    95,
		MemoryPercent: 50,
		PodCount:      3,
		PodRunning:    3,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 查询异常列表
	q := model.AnomalyQuery{Page: 1, PageSize: 10}
	result, _ := svc.ListAnomalies(ctx, q)
	if result.Total < 1 {
		t.Fatal("应检测到 CPU 异常")
	}

	// 应该有 error 级别的 high_cpu
	found := false
	for _, a := range result.Items {
		if a.AnomalyType == model.AnomalyHighCPU && a.Level == model.AnomalyError {
			found = true
			break
		}
	}
	if !found {
		t.Error("应检测到 error 级别的 CPU 过高异常")
	}
}

func TestDetectHighMemory(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)

	// 内存 80% → 警告
	svc.ReportMetrics(ctx, &model.ResourceMetrics{
		SessionID:     1,
		Namespace:     "sandbox-1",
		CPUPercent:    30,
		MemoryPercent: 80,
		PodCount:      3,
		PodRunning:    3,
	})

	result, _ := svc.ListAnomalies(ctx, model.AnomalyQuery{Page: 1, PageSize: 10})
	found := false
	for _, a := range result.Items {
		if a.AnomalyType == model.AnomalyHighMemory && a.Level == model.AnomalyWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("应检测到 warning 级别的内存过高异常")
	}
}

func TestDetectAbnormalPods(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)

	// 2 个异常 Pod → 严重
	svc.ReportMetrics(ctx, &model.ResourceMetrics{
		SessionID:     1,
		Namespace:     "sandbox-1",
		CPUPercent:    30,
		MemoryPercent: 30,
		PodCount:      5,
		PodRunning:    3,
		PodAbnormal:   2,
	})

	result, _ := svc.ListAnomalies(ctx, model.AnomalyQuery{Page: 1, PageSize: 10})
	found := false
	for _, a := range result.Items {
		if a.AnomalyType == model.AnomalyFailedPod && a.Level == model.AnomalyError {
			found = true
			break
		}
	}
	if !found {
		t.Error("应检测到 error 级别的 Pod 异常")
	}
}

func TestAlertCallback(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)

	var alerted bool
	var alertAnomaly *model.SandboxAnomaly
	svc.SetAlertCallback(func(_ context.Context, a *model.SandboxAnomaly) {
		alerted = true
		alertAnomaly = a
	})

	// CPU 95% → 触发告警
	svc.ReportMetrics(ctx, &model.ResourceMetrics{
		SessionID:     1,
		Namespace:     "sandbox-1",
		CPUPercent:    95,
		MemoryPercent: 50,
		PodCount:      3,
		PodRunning:    3,
	})

	// 给 goroutine 一点时间
	time.Sleep(10 * time.Millisecond)

	if !alerted {
		t.Error("应触发告警回调")
	}
	if alertAnomaly == nil || alertAnomaly.AnomalyType != model.AnomalyHighCPU {
		t.Error("告警类型应为 CPU 过高")
	}
}

// ——— 状态卡住检测 ———

func TestDetectStuckStatus(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	// 创建一个 15 分钟前的 creating 状态沙箱（超过默认 10 分钟阈值）
	sb := &model.Sandbox{
		SessionID: 1,
		UserID:    100,
		ProjectID: 1,
		Namespace: "sandbox-1",
		Status:    model.StatusCreating,
		CreatedAt: time.Now().Add(-15 * time.Minute),
	}
	store.Create(ctx, sb)

	anomalies, err := svc.DetectStuckStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(anomalies) == 0 {
		t.Error("应检测到状态卡住的沙箱")
	}
	if anomalies[0].AnomalyType != model.AnomalyStuckStatus {
		t.Errorf("异常类型应为 stuck_status, got %s", anomalies[0].AnomalyType)
	}
}

// ——— 长时间运行检测 ———

func TestDetectLongRunning(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	// 创建一个 100 分钟前的 running 沙箱（超过 90 分钟警告阈值）
	sb := &model.Sandbox{
		SessionID: 1,
		UserID:    100,
		ProjectID: 1,
		Namespace: "sandbox-1",
		Status:    model.StatusRunning,
		CreatedAt: time.Now().Add(-100 * time.Minute),
	}
	store.Create(ctx, sb)

	anomalies, err := svc.DetectLongRunning(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(anomalies) == 0 {
		t.Error("应检测到长时间运行的沙箱")
	}
	if anomalies[0].Level != model.AnomalyWarning {
		t.Errorf("级别应为 warning, got %s", anomalies[0].Level)
	}
}

// ——— 手动释放测试 ———

func TestReleaseSandbox(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 42, 100, 1, model.StatusRunning)

	err := svc.ReleaseSandbox(ctx, 42, "手动释放")
	if err != nil {
		t.Fatalf("释放失败: %v", err)
	}

	// 验证状态变为 releasing
	sb, _ := store.GetBySessionID(ctx, 42)
	if sb.Status != model.StatusReleasing {
		t.Errorf("状态应为 releasing, got %s", sb.Status)
	}

	// 重复释放应报错
	err = svc.ReleaseSandbox(ctx, 42, "再次释放")
	if err == nil {
		t.Error("重复释放应报错")
	}
}

// ——— 异常管理测试 ———

func TestResolveAnomaly(t *testing.T) {
	svc, store := newTestMonitorService()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)

	// 产生一个异常
	svc.ReportMetrics(ctx, &model.ResourceMetrics{
		SessionID:     1,
		Namespace:     "sandbox-1",
		CPUPercent:    95,
		MemoryPercent: 50,
		PodCount:      3,
		PodRunning:    3,
	})

	// 获取异常 ID
	result, _ := svc.ListAnomalies(ctx, model.AnomalyQuery{Page: 1, PageSize: 10})
	if result.Total == 0 {
		t.Fatal("应至少有一个异常")
	}
	anomalyID := result.Items[0].ID

	// 标记已解决
	err := svc.ResolveAnomaly(ctx, anomalyID)
	if err != nil {
		t.Fatalf("解决异常失败: %v", err)
	}

	// 验证：筛选未解决的，应该 0 个
	unresolved := false
	result2, _ := svc.ListAnomalies(ctx, model.AnomalyQuery{
		Resolved: &unresolved,
		Page:     1,
		PageSize: 10,
	})
	if result2.Total != 0 {
		t.Errorf("未解决的异常应为 0, got %d", result2.Total)
	}
}

// ——— 模型辅助方法测试 ———

func TestSandboxRuntimeAndRemaining(t *testing.T) {
	now := time.Now()
	sb := &model.Sandbox{
		Status:         model.StatusRunning,
		CreatedAt:      now.Add(-30 * time.Minute),
		LastActivityAt: now.Add(-10 * time.Minute),
	}

	runtime := sb.ComputeRuntime(now)
	if runtime < 1700 || runtime > 1900 { // 约 30 分钟 = 1800 秒
		t.Errorf("运行时长应约 1800 秒, got %d", runtime)
	}

	// 剩余按活跃度租期算：距最近活动过了 10 分钟，默认租期 30 分钟 → 剩余约 1200 秒。
	remaining := sb.ComputeRemaining(now)
	if want := int64((model.ActivityLease - 10*time.Minute).Seconds()); remaining != want {
		t.Errorf("剩余时间应约 %d 秒, got %d", want, remaining)
	}

	// 一直活跃但已逼近绝对上限时，剩余取两者更早的那个。
	nearCeiling := &model.Sandbox{
		Status:         model.StatusRunning,
		CreatedAt:      now.Add(-model.MaxSandboxLifetime + 5*time.Minute),
		LastActivityAt: now.Add(-time.Minute),
	}
	if r := nearCeiling.ComputeRemaining(now); r < 250 || r > 310 {
		t.Errorf("应被绝对上限收敛到约 300 秒, got %d", r)
	}

	// 停手超过租期后剩余为负，回收循环据此判过期。
	idle := &model.Sandbox{
		Status:         model.StatusRunning,
		CreatedAt:      now.Add(-2 * time.Hour),
		LastActivityAt: now.Add(-40 * time.Minute),
	}
	if r := idle.ComputeRemaining(now); r >= 0 {
		t.Errorf("超出租期后剩余应为负, got %d", r)
	}
}

func TestSandboxIsActiveState(t *testing.T) {
	activeStatuses := []string{
		model.StatusCreating,
		model.StatusDeploying,
		model.StatusReady,
		model.StatusRunning,
		model.StatusReleasing,
	}
	for _, status := range activeStatuses {
		sb := &model.Sandbox{Status: status}
		if !sb.IsActiveState() {
			t.Errorf("%s 应被视为活跃状态", status)
		}
	}

	inactiveStatuses := []string{
		model.StatusReleased,
		model.StatusFailed,
	}
	for _, status := range inactiveStatuses {
		sb := &model.Sandbox{Status: status}
		if sb.IsActiveState() {
			t.Errorf("%s 应被视为非活跃状态", status)
		}
	}
}

// ——— 仓储层测试 ———

func TestInMemoryStore_CountByStatus(t *testing.T) {
	store := service.NewInMemoryMonitorStore()
	ctx := context.Background()

	createTestSandbox(store, 1, 100, 1, model.StatusRunning)
	createTestSandbox(store, 2, 100, 1, model.StatusReady)
	createTestSandbox(store, 3, 100, 1, model.StatusReleased)

	byStatus, err := store.CountByStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if byStatus[model.StatusRunning] != 1 {
		t.Errorf("running 应为 1, got %d", byStatus[model.StatusRunning])
	}
	if byStatus[model.StatusReady] != 1 {
		t.Errorf("ready 应为 1, got %d", byStatus[model.StatusReady])
	}
	if byStatus[model.StatusReleased] != 1 {
		t.Errorf("released 应为 1, got %d", byStatus[model.StatusReleased])
	}
}
