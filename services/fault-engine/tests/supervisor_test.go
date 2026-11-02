package tests

import (
	"context"
	"testing"
	"time"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
)

// 一个按时间序列返回指标的 fetcher：第 N 次调用返回 frames[N]。
type scriptedFetcher struct {
	frames [][]service.JudgeMetricSample
	calls  int
}

func (f *scriptedFetcher) Fetch(_ context.Context, _ string, _ []string) ([]service.JudgeMetricSample, error) {
	i := f.calls
	f.calls++
	if i >= len(f.frames) {
		return f.frames[len(f.frames)-1], nil
	}
	return f.frames[i], nil
}

// TestSupervisorDrivesToResolved 直接驱动 Tick：立即注入->高CPU->恢复且持续稳定->终态。
func TestSupervisorDrivesToResolved(t *testing.T) {
	exec := &fakeExecutor{}
	clock := &fakeClock{t: time.Now()}
	scn := delayScenario(0) // delay 0
	orch := service.NewOrchestrator(service.OrchestratorConfig{
		SessionID:      7001,
		Namespace:      "sandbox-7001",
		Scenario:       scn,
		Injector:       service.NewInjector(exec),
		StableRequired: 10 * time.Second,
		Clock:          clock,
	})

	// 指标：先高，后续恢复
	fetcher := &scriptedFetcher{frames: [][]service.JudgeMetricSample{
		cpuMetric(95), // 第1次：注入时
		cpuMetric(95), // 第2次：仍故障
		cpuMetric(40), // 第3次起：恢复
	}}
	sup := service.NewFaultSupervisor(service.SupervisorConfig{
		Orchestrator: orch,
		Fetcher:      fetcher,
		Clock:        clock,
		Namespace:    "sandbox-7001",
		StartedAt:    clock.Now(),
		Metrics:      []string{"cpu_usage"},
	})

	ctx := context.Background()

	// 第1轮：delay=0，立即注入（injected）
	if stopped := sup.Tick(ctx); stopped {
		t.Fatal("注入后不应停止")
	}
	if orch.Phase() != service.PhaseInjected || orch.ExperimentUID() == "" {
		t.Fatalf("首轮应注入，phase=%s", orch.Phase())
	}
	// 第2轮：CPU仍高，保持
	clock.Add(5 * time.Second)
	if sup.Tick(ctx) {
		t.Fatal("指标异常不应停止")
	}
	if orch.Phase() != service.PhaseInjected {
		t.Fatalf("应保持injected，实际 %s", orch.Phase())
	}
	// 第3轮：首次恢复，开始稳定计时（0s），尚不足窗口
	clock.Add(5 * time.Second)
	if sup.Tick(ctx) {
		t.Fatal("首次达标不应立即终态")
	}
	// 第4轮：持续恢复超过10s窗口 -> revoked，Tick 返回 true
	clock.Add(11 * time.Second)
	if stopped := sup.Tick(ctx); !stopped {
		t.Fatal("持续达标后应进入终态并停止")
	}
	if orch.Phase() != service.PhaseRevoked {
		t.Fatalf("应 revoked，实际 %s", orch.Phase())
	}
}

// TestSupervisorStopAborts 外部 Stop 时若已注入必须撤销。
func TestSupervisorStopAborts(t *testing.T) {
	exec := &fakeExecutor{}
	injected := make(chan struct{})
	clock := &fakeClock{t: time.Now()}
	scn := delayScenario(0)
	orch := service.NewOrchestrator(service.OrchestratorConfig{
		SessionID: 7002, Namespace: "sandbox-7002", Scenario: scn,
		Injector: service.NewInjector(exec), StableRequired: time.Minute, Clock: clock,
		Events: service.EventHandler{OnInjected: func(context.Context, *model.Experiment) { close(injected) }},
	})
	fetcher := &scriptedFetcher{frames: [][]service.JudgeMetricSample{cpuMetric(95)}}
	sup := service.NewFaultSupervisor(service.SupervisorConfig{
		Orchestrator: orch, Fetcher: fetcher, Clock: clock,
		Interval:  time.Hour, // 很长，主动 Stop 测试
		Namespace: "sandbox-7002", StartedAt: clock.Now(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)
	t.Cleanup(func() {
		sup.Stop()
		select {
		case <-sup.Done():
		case <-time.After(5 * time.Second):
			t.Error("supervisor 未在停止后退出")
		}
	})

	// 等首轮（立即）注入
	select {
	case <-injected:
	case <-time.After(5 * time.Second):
		t.Fatal("未收到首轮注入事件")
	}
	if orch.Phase() != service.PhaseInjected {
		t.Fatalf("首轮应注入，实际 %s", orch.Phase())
	}
	sup.Stop()
	<-sup.Done()
	if orch.Phase() != service.PhaseRevoked {
		t.Fatalf("Stop 后应 revoked，实际 %s", orch.Phase())
	}
}

// ---- Registry 用同步 fake runner 测，避免真实 goroutine 时序 ----

type fakeRunner struct {
	phase  service.Phase
	stopCh chan struct{}
	doneCh chan struct{}
	stops  int
}

func newFakeRunner(phase service.Phase) *fakeRunner {
	return &fakeRunner{phase: phase, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
}
func (r *fakeRunner) Run(_ context.Context) {}
func (r *fakeRunner) Stop() {
	r.stops++
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
	close(r.doneCh)
}
func (r *fakeRunner) Done() <-chan struct{} { return r.doneCh }
func (r *fakeRunner) Phase() Phase          { return r.phase }

// 别名：fakeRunner 的 Phase 返回类型
type Phase = service.Phase

func TestRegistryStartStopCleanup(t *testing.T) {
	reg := service.NewSupervisorRegistry()
	ctx := context.Background()
	rn := newFakeRunner(service.PhaseInjected)

	if err := reg.Start(ctx, 8001, rn); err != nil {
		t.Fatal(err)
	}
	if !reg.Has(8001) || reg.ActiveCount() != 1 {
		t.Fatal("启动后应存在")
	}
	if err := reg.Start(ctx, 8001, rn); err == nil {
		t.Fatal("重复启动同一会话应报错")
	}
	if !reg.StopSession(8001) {
		t.Fatal("StopSession 应返回 true")
	}
	if rn.stops != 1 {
		t.Fatal("应调用 runner.Stop 一次")
	}
	// runner 已 done，Cleanup 移除
	removed := reg.Cleanup()
	if len(removed) != 1 || removed[0] != 8001 {
		t.Fatalf("应清理8001，实际 %v", removed)
	}
	if reg.ActiveCount() != 0 {
		t.Fatal("清理后应为0")
	}
}

func TestRegistryStopAll(t *testing.T) {
	reg := service.NewSupervisorRegistry()
	ctx := context.Background()
	r1 := newFakeRunner(service.PhaseInjected)
	r2 := newFakeRunner(service.PhaseArmed)
	_ = reg.Start(ctx, 9001, r1)
	_ = reg.Start(ctx, 9002, r2)
	reg.StopAll()
	if r1.stops != 1 || r2.stops != 1 {
		t.Fatal("StopAll 应停止所有 runner")
	}
}
