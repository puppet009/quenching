package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/repository"
	"github.com/quenching/fault-engine/internal/service"
)

// runtimeFixture 运行时服务测试夹具：内存仓储 + 假执行器，无需集群/数据库。
type runtimeFixture struct {
	svc        *service.RuntimeService
	exec       *fakeExecutor
	defectRepo *repository.InMemoryDefectRepo
	expRepo    *repository.InMemoryExperimentRepo
	registry   *service.SupervisorRegistry
}

// newRuntimeFixture 构造带默认命名空间的运行时服务。
// 轮询周期故意设为 1 小时，避免测试期间真实 supervisor goroutine 多跑一轮。
func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	return newRuntimeFixtureNS(t, "sandbox")
}

// newRuntimeFixtureNS 构造指定默认命名空间的运行时服务（空串模拟未配置）。
// fakeFetcher 必须预置至少一帧，空帧集越界访问会 panic（见 fake_fetcher_test.go）。
func newRuntimeFixtureNS(t *testing.T, defaultNS string) *runtimeFixture {
	t.Helper()
	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	exec := &fakeExecutor{}
	defectRepo := repository.NewInMemoryDefectRepo()
	expRepo := repository.NewInMemoryExperimentRepo()
	registry := service.NewSupervisorRegistry()
	fetcher := &fakeFetcher{frames: [][]service.JudgeMetricSample{{}}}
	svc := service.NewRuntimeService(rootCtx, exec, defectRepo, expRepo, registry, fetcher, service.RuntimeConfig{
		DefaultNamespace: defaultNS,
		PollInterval:     time.Hour,
		StableRequired:   time.Second,
	})
	t.Cleanup(func() {
		registry.StopAll()
		svc.Close()
	})
	return &runtimeFixture{svc: svc, exec: exec, defectRepo: defectRepo, expRepo: expRepo, registry: registry}
}

// seedScenarioDefect 把 validScenarioJSON 作为 amplifier_config 植入内存剧本仓储。
func (f *runtimeFixture) seedScenarioDefect(t *testing.T, projectID int64, code string) {
	t.Helper()
	var cfg model.JSONMap
	if err := json.Unmarshal([]byte(validScenarioJSON), &cfg); err != nil {
		t.Fatalf("剧本配置反序列化失败: %v", err)
	}
	defect := &model.Defect{
		ProjectID:       projectID,
		Code:            code,
		Name:            "CPU满载剧本",
		DefectType:      "code",
		Difficulty:      "hard",
		IsActive:        true,
		AmplifierConfig: cfg,
	}
	if err := f.defectRepo.Create(context.Background(), defect); err != nil {
		t.Fatalf("植入剧本失败: %v", err)
	}
}

// waitUntil 轮询断言直到成功或超时，用于等待异步编排/补偿 goroutine 落库。
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时未满足")
}

// ——— InjectOnce 临时链路（T3-12）———

func TestRuntimeInjectOnceSuccess(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()

	exp, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID:  1001,
		Kind:       string(model.KindCPU),
		Namespace:  "team-a",
		Pod:        "order-svc-abc",
		CPUPercent: 80,
	})
	if err != nil {
		t.Fatalf("临时注入失败: %v", err)
	}
	if exp.Source != model.SourceConsequence {
		t.Fatalf("来源应为 consequence，实际 %s", exp.Source)
	}
	if exp.Status != model.StatusRunning {
		t.Fatalf("返回状态应为 running，实际 %s", exp.Status)
	}
	if exp.ExperimentUID != "fake-uid-1" {
		t.Fatalf("uid 回写错误，实际 %s", exp.ExperimentUID)
	}
	if exp.Target != "team-a/order-svc-abc" {
		t.Fatalf("目标描述错误，实际 %s", exp.Target)
	}
	if exp.Spec.TargetNamespace != "team-a" || exp.Spec.CPUPercent != 80 {
		t.Fatal("注入规格字段透传错误")
	}

	// 仓储状态与 uid 一致。
	stored, err := f.expRepo.GetByUID(ctx, 1001, "fake-uid-1")
	if err != nil {
		t.Fatalf("按 uid 查询登记记录失败: %v", err)
	}
	if stored.Status != model.StatusRunning {
		t.Fatalf("仓储状态应为 running，实际 %s", stored.Status)
	}

	// blade create 命令已下发。
	args := f.exec.lastArgs()
	if len(args) == 0 || args[0] != "create" {
		t.Fatalf("应执行 blade create，实际 %v", args)
	}
}

func TestRuntimeInjectOnceNamespaceFallback(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()

	// 请求不带命名空间：回落全局默认。
	exp, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID:  1002,
		Kind:       string(model.KindCPU),
		CPUPercent: 50,
	})
	if err != nil {
		t.Fatalf("回落默认命名空间注入失败: %v", err)
	}
	if exp.Spec.TargetNamespace != "sandbox" {
		t.Fatalf("应回落 sandbox，实际 %s", exp.Spec.TargetNamespace)
	}
	if exp.Target != "sandbox" {
		t.Fatalf("无 pod 时目标应为纯命名空间，实际 %s", exp.Target)
	}

	// 请求显式命名空间优先于默认值。
	exp2, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID:  1003,
		Kind:       string(model.KindCPU),
		Namespace:  "team-b",
		CPUPercent: 50,
	})
	if err != nil {
		t.Fatalf("显式命名空间注入失败: %v", err)
	}
	if exp2.Spec.TargetNamespace != "team-b" {
		t.Fatalf("显式命名空间应优先，实际 %s", exp2.Spec.TargetNamespace)
	}
}

func TestRuntimeInjectOnceValidation(t *testing.T) {
	f := newRuntimeFixtureNS(t, "") // 未配置默认命名空间
	ctx := context.Background()

	bad := []struct {
		name string
		req  service.InjectOnceReq
		want string
	}{
		{"缺会话ID", service.InjectOnceReq{Kind: string(model.KindCPU), Namespace: "sandbox"}, "会话ID"},
		{"预留类型拒绝", service.InjectOnceReq{SessionID: 1, Kind: string(model.KindThreadpool), Namespace: "sandbox"}, "暂不支持注入"},
		{"未知类型拒绝", service.InjectOnceReq{SessionID: 1, Kind: "alien", Namespace: "sandbox"}, "暂不支持注入"},
		{"命名空间全空", service.InjectOnceReq{SessionID: 1, Kind: string(model.KindCPU)}, "目标命名空间不能为空"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.svc.InjectOnce(ctx, c.req)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%s 应报含 %q 的错误，实际 %v", c.name, c.want, err)
			}
			// 校验失败不应产生任何执行与登记。
			if len(f.exec.CallsSnapshot()) != 0 {
				t.Fatalf("%s 不应执行 blade 命令", c.name)
			}
		})
	}
}

func TestRuntimeInjectOnceFailureMarkedFailed(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()
	f.exec.FailNextOnce()

	_, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID:  1004,
		Kind:       string(model.KindCPU),
		Namespace:  "sandbox",
		CPUPercent: 90,
	})
	if err == nil {
		t.Fatal("执行器失败时注入应返回错误")
	}

	active, err := f.expRepo.ListActiveBySession(ctx, 1004)
	if err != nil {
		t.Fatalf("查询活跃实验失败: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("注入失败后不应残留活跃记录，实际 %d 条", len(active))
	}
	all, _ := f.expRepo.ListBySession(ctx, 1004)
	if len(all) != 1 || all[0].Status != model.StatusFailed {
		t.Fatalf("应有一条 failed 记录，实际 %+v", all)
	}
	if !strings.Contains(all[0].FailReason, "模拟执行失败") {
		t.Fatalf("失败原因未透传，实际 %s", all[0].FailReason)
	}
}

// ——— RevokeOne 临时链路撤销 ———

func TestRuntimeRevokeOneSuccess(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()

	exp, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID:  1101,
		Kind:       string(model.KindMem),
		Namespace:  "sandbox",
		Pod:        "pay-svc",
		MemPercent: 60,
	})
	if err != nil {
		t.Fatalf("前置注入失败: %v", err)
	}

	if err := f.svc.RevokeOne(ctx, 1101, exp.ExperimentUID); err != nil {
		t.Fatalf("按 uid 撤销失败: %v", err)
	}
	stored, err := f.expRepo.GetByUID(ctx, 1101, exp.ExperimentUID)
	if err != nil {
		t.Fatalf("撤销后查询失败: %v", err)
	}
	if stored.Status != model.StatusRevoked {
		t.Fatalf("状态应为 revoked，实际 %s", stored.Status)
	}
	args := f.exec.lastArgs()
	if len(args) == 0 || args[0] != "destroy" {
		t.Fatalf("应执行 blade destroy，实际 %v", args)
	}

	// 撤销幂等：已 revoked 的记录重复撤销不报错。
	if err := f.svc.RevokeOne(ctx, 1101, exp.ExperimentUID); err != nil {
		t.Fatalf("重复撤销应幂等，实际 %v", err)
	}
}

func TestRuntimeRevokeOneErrors(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()

	if err := f.svc.RevokeOne(ctx, 0, "u"); err == nil {
		t.Fatal("会话ID为 0 应报错")
	}
	if err := f.svc.RevokeOne(ctx, 1, ""); err == nil {
		t.Fatal("uid 为空应报错")
	}
	// 存在性校验：无此前注入记录。
	if err := f.svc.RevokeOne(ctx, 9999, "not-exist"); err != repository.ErrExperimentNotFound {
		t.Fatalf("不存在的 uid 应返回 ErrExperimentNotFound，实际 %v", err)
	}

	// 会话归属隔离：会话 A 的 uid 不能被会话 B 撤销。
	exp, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID: 1201, Kind: string(model.KindCPU), Namespace: "sandbox", CPUPercent: 30,
	})
	if err != nil {
		t.Fatalf("前置注入失败: %v", err)
	}
	if err := f.svc.RevokeOne(ctx, 1202, exp.ExperimentUID); err != repository.ErrExperimentNotFound {
		t.Fatalf("跨会话撤销应被归属校验拦截，实际 %v", err)
	}
}

// ——— StopSession 会话结束补偿 ———

func TestRuntimeStopSessionCompensation(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()
	const sessionID int64 = 1301

	// 仅临时链路：两条注入（模拟踩坑多次后果传导）。
	if _, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID: sessionID, Kind: string(model.KindCPU), Namespace: "sandbox", CPUPercent: 70,
	}); err != nil {
		t.Fatalf("注入 1 失败: %v", err)
	}
	if _, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID: sessionID, Kind: string(model.KindNetDelay), Namespace: "sandbox", Pod: "p", DelayMillis: 200,
	}); err != nil {
		t.Fatalf("注入 2 失败: %v", err)
	}

	if err := f.svc.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("停止会话失败: %v", err)
	}

	all, _ := f.expRepo.ListBySession(ctx, sessionID)
	if len(all) != 2 {
		t.Fatalf("应有 2 条记录，实际 %d", len(all))
	}
	for _, e := range all {
		if e.Status != model.StatusRevoked {
			t.Fatalf("全部应被补偿撤销，uid=%s 实际 %s", e.ExperimentUID, e.Status)
		}
	}

	// 幂等：无编排、无活跃记录时再次结束不报错。
	if err := f.svc.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("重复结束会话应幂等，实际 %v", err)
	}
	// 不存在编排器的会话同样安全。
	if err := f.svc.StopSession(ctx, 1399); err != nil {
		t.Fatalf("空会话结束不应报错，实际 %v", err)
	}
	if err := f.svc.StopSession(ctx, 0); err == nil {
		t.Fatal("会话ID为 0 应报错")
	}
}

// ——— StartScenario 剧本链路（T2-13）———

func TestRuntimeStartScenarioRegistered(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()
	const (
		projectID int64 = 7
		sessionID int64 = 2001
		code            = "cpu-fullload-01"
	)
	f.seedScenarioDefect(t, projectID, code)

	exp, err := f.svc.StartScenario(ctx, service.StartScenarioReq{
		SessionID: sessionID,
		ProjectID: projectID,
		Code:      code,
	})
	if err != nil {
		t.Fatalf("启动剧本编排失败: %v", err)
	}
	if exp.Source != model.SourceScenario {
		t.Fatalf("来源应为 scenario，实际 %s", exp.Source)
	}
	if exp.Status != model.StatusCreated {
		t.Fatalf("启动瞬间应为 created（延迟触发尚未注入），实际 %s", exp.Status)
	}
	if exp.Kind != model.KindCPU {
		t.Fatalf("故障类型错误，实际 %s", exp.Kind)
	}
	if exp.Target != "sandbox/order-svc" {
		t.Fatalf("目标应由命名空间+剧本 target.pod 组装，实际 %s", exp.Target)
	}
	if !f.registry.Has(sessionID) {
		t.Fatal("注册表应持有该会话的编排器")
	}
	// delay=30s 且启动首轮 elapsed=0，不应有任何 blade 调用。
	if len(f.exec.CallsSnapshot()) != 0 {
		t.Fatalf("延迟触发期不应注入，实际调用 %v", f.exec.CallsSnapshot())
	}

	// 结束会话：Abort 编排器 + 补偿撤销；created 行由 finalizeWhenDone 兜底置 revoked。
	if err := f.svc.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("停止会话失败: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		list, _ := f.expRepo.ListBySession(ctx, sessionID)
		return len(list) == 1 && list[0].Status == model.StatusRevoked
	})
}

func TestRuntimeStartScenarioDuplicateSession(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()
	const (
		projectID int64 = 7
		sessionID int64 = 2002
		code            = "cpu-fullload-01"
	)
	f.seedScenarioDefect(t, projectID, code)

	req := service.StartScenarioReq{SessionID: sessionID, ProjectID: projectID, Code: code}
	if _, err := f.svc.StartScenario(ctx, req); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	_, err := f.svc.StartScenario(ctx, req)
	if err == nil || !strings.Contains(err.Error(), "已在运行") {
		t.Fatalf("同会话重复启动应被注册表拒绝，实际 %v", err)
	}
	// 编排没启动：登记行应被标记失败，避免悬挂 created。
	list, _ := f.expRepo.ListBySession(ctx, sessionID)
	if len(list) != 2 {
		t.Fatalf("应有两条登记，实际 %d", len(list))
	}
	if list[1].Status != model.StatusFailed {
		t.Fatalf("重复启动对应的登记应为 failed，实际 %s", list[1].Status)
	}
}

func TestRuntimeStartScenarioValidation(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()
	const (
		projectID int64 = 7
		code            = "cpu-fullload-01"
	)
	f.seedScenarioDefect(t, projectID, code)

	bad := []struct {
		name string
		req  service.StartScenarioReq
		want string
	}{
		{"缺会话ID", service.StartScenarioReq{ProjectID: projectID, Code: code}, "会话ID"},
		{"缺项目ID", service.StartScenarioReq{SessionID: 1, Code: code}, "项目ID"},
		{"缺剧本编码", service.StartScenarioReq{SessionID: 1, ProjectID: projectID}, "剧本编码"},
		{"剧本不存在", service.StartScenarioReq{SessionID: 1, ProjectID: projectID, Code: "nope"}, "不存在"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.svc.StartScenario(ctx, c.req)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%s 应报含 %q 的错误，实际 %v", c.name, c.want, err)
			}
		})
	}

	// 未配置默认命名空间且请求未指定：拒绝启动。
	emptyNS := newRuntimeFixtureNS(t, "")
	emptyNS.seedScenarioDefect(t, projectID, code)
	_, err := emptyNS.svc.StartScenario(ctx, service.StartScenarioReq{SessionID: 2100, ProjectID: projectID, Code: code})
	if err == nil || !strings.Contains(err.Error(), "目标命名空间不能为空") {
		t.Fatalf("命名空间全空应拒绝，实际 %v", err)
	}
}

func TestRuntimeListExperiments(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()

	if list, err := f.svc.ListExperiments(ctx, 3001); err != nil || len(list) != 0 {
		t.Fatalf("空会话应返回空列表，list=%v err=%v", list, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
			SessionID: 3001, Kind: string(model.KindCPU), Namespace: "sandbox", CPUPercent: 40,
		}); err != nil {
			t.Fatalf("前置注入失败: %v", err)
		}
	}
	if _, err := f.svc.InjectOnce(ctx, service.InjectOnceReq{
		SessionID: 3002, Kind: string(model.KindCPU), Namespace: "sandbox", CPUPercent: 40,
	}); err != nil {
		t.Fatalf("他会话注入失败: %v", err)
	}

	list, err := f.svc.ListExperiments(ctx, 3001)
	if err != nil {
		t.Fatalf("查询注入历史失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应只返回本会话 2 条，实际 %d", len(list))
	}
	if list[0].ID >= list[1].ID {
		t.Fatal("注入历史应按 ID 升序")
	}
	if _, err := f.svc.ListExperiments(ctx, 0); err == nil {
		t.Fatal("会话ID为 0 应报错")
	}
}
