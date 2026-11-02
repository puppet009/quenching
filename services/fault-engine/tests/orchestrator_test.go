package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
)

// fakeClock 可控时钟。
type fakeClock struct{ t time.Time }

// 查询阶段/实验信息与驱动循环并行，事件回调也允许查询，不能持锁调用回调。
func TestOrchestratorConcurrentSnapshots(t *testing.T) {
	for i := 0; i < 20; i++ {
		var orch *service.Orchestrator
		orch = newOrch(t, &fakeExecutor{}, delayScenario(0), time.Second, &fakeClock{t: time.Now()}, service.EventHandler{
			OnInjected: func(context.Context, *model.Experiment) {
				if orch.Phase() != service.PhaseInjected || orch.ExperimentUID() == "" {
					t.Error("注入回调应可查询已提交的阶段和实验")
				}
			},
			OnRevoked: func(context.Context, int64) {
				if orch.Phase() != service.PhaseRevoked {
					t.Error("撤销回调应可查询终态")
				}
			},
		})
		var readers sync.WaitGroup
		ready := make(chan struct{}, 4)
		start := make(chan struct{})
		for j := 0; j < 4; j++ {
			readers.Add(1)
			go func() {
				defer readers.Done()
				ready <- struct{}{}
				<-start
				for k := 0; k < 1000; k++ {
					_ = orch.Phase()
					_ = orch.ExperimentUID()
				}
			}()
		}
		for j := 0; j < 4; j++ {
			<-ready
		}
		close(start)
		phase, stepErr := orch.Step(context.Background(), 0, cpuMetric(95))
		abortErr := orch.Abort(context.Background())
		readers.Wait()
		if stepErr != nil || phase != service.PhaseInjected || abortErr != nil || orch.Phase() != service.PhaseRevoked {
			t.Fatalf("生命周期异常: phase=%s step=%v abort=%v", phase, stepErr, abortErr)
		}
	}
}

func (c *fakeClock) Now() time.Time      { return c.t }
func (c *fakeClock) Add(d time.Duration) { c.t = c.t.Add(d) }

// 构造延迟触发的 CPU 故障剧本。
func delayScenario(delaySec int) *service.Scenario {
	raw := fmt.Sprintf(`{
  "code":"cpu-test","kind":"cpu","title":"CPU",
  "target":{"pod":"order-svc"},
  "spec":{"kind":"cpu","cpu_percent":100},
  "trigger":{"type":"delay","delay_seconds":%d},
  "recovery":[{"metric":"cpu_usage","service":"order-svc","op":"lte","value":70}],
  "auto_revoke":true}`, delaySec)
	return service.MustParseScenarioJSON(raw)
}

func newOrch(t *testing.T, exec *fakeExecutor, scn *service.Scenario, stable time.Duration, clock service.Clock, events service.EventHandler) *service.Orchestrator {
	t.Helper()
	return service.NewOrchestrator(service.OrchestratorConfig{
		SessionID:      5001,
		Namespace:      "sandbox-5001",
		Scenario:       scn,
		Injector:       service.NewInjector(exec),
		StableRequired: stable,
		Clock:          clock,
		Events:         events,
	})
}

func cpuMetric(v float64) []service.JudgeMetricSample {
	return []service.JudgeMetricSample{{Name: "cpu_usage", Service: "order-svc", Value: v}}
}

// TestOrchestratorFullLifecycle 触发->注入->故障中->持续恢复达标->自动撤销 完整走一遍。
func TestOrchestratorFullLifecycle(t *testing.T) {
	exec := &fakeExecutor{}
	clock := &fakeClock{t: time.Now()}
	var injected, resolved, revoked bool
	orch := newOrch(t, exec, delayScenario(0), 30*time.Second, clock, service.EventHandler{
		OnInjected: func(_ context.Context, e *model.Experiment) { injected = e.ExperimentUID != "" },
		OnResolved: func(_ context.Context, _ int64) { resolved = true },
		OnRevoked:  func(_ context.Context, _ int64) { revoked = true },
	})
	ctx := context.Background()

	// 延迟0秒：第一步立即注入
	if p, err := orch.Step(ctx, 0, cpuMetric(95)); err != nil || p != service.PhaseInjected {
		t.Fatalf("应injected，phase=%s err=%v", p, err)
	}
	if !injected || orch.ExperimentUID() == "" {
		t.Fatal("注入事件/uid 异常")
	}
	// 故障期间高 CPU
	if p, _ := orch.Step(ctx, 10, cpuMetric(95)); p != service.PhaseInjected {
		t.Fatalf("指标异常应保持injected，实际 %s", p)
	}
	// 首次达标：开始稳定计时（0s），不判定
	clock.Add(10 * time.Second)
	if p, _ := orch.Step(ctx, 20, cpuMetric(40)); p != service.PhaseInjected {
		t.Fatalf("首次达标在窗口内应保持，实际 %s", p)
	}
	// 持续达标 31s -> resolved + 自动 revoke
	clock.Add(31 * time.Second)
	if p, err := orch.Step(ctx, 51, cpuMetric(40)); err != nil || p != service.PhaseRevoked {
		t.Fatalf("持续达标后应revoked，phase=%s err=%v", p, err)
	}
	if !resolved || !revoked {
		t.Fatalf("应触发resolved和revoked回调，resolved=%v revoked=%v", resolved, revoked)
	}
}

// TestOrchestratorWaitBeforeDelay 未到延迟时间保持 armed，不注入。
func TestOrchestratorWaitBeforeDelay(t *testing.T) {
	exec := &fakeExecutor{}
	clock := &fakeClock{t: time.Now()}
	orch := newOrch(t, exec, delayScenario(30), time.Second, clock, service.EventHandler{})
	if p, _ := orch.Step(context.Background(), 10, nil); p != service.PhaseArmed {
		t.Fatalf("未到30s应armed，实际 %s", p)
	}
	if len(exec.calls) != 0 {
		t.Fatal("armed阶段不应执行注入")
	}
}

// TestOrchestratorWindowSkipped 错过触发窗口 -> skipped，不注入。
func TestOrchestratorWindowSkipped(t *testing.T) {
	exec := &fakeExecutor{}
	scn := service.MustParseScenarioJSON(`{
  "code":"win","kind":"cpu",
  "trigger":{"type":"window","window":{"start_seconds":10,"end_seconds":20}},
  "recovery":[{"metric":"cpu_usage","op":"lte","value":70}]}`)
	clock := &fakeClock{t: time.Now()}
	orch := newOrch(t, exec, scn, time.Second, clock, service.EventHandler{})
	if p, _ := orch.Step(context.Background(), 25, nil); p != service.PhaseSkipped {
		t.Fatalf("错过窗口应skipped，实际 %s", p)
	}
	if len(exec.calls) != 0 {
		t.Fatal("skipped不应注入")
	}
}

// TestOrchestratorAbortRevokes 已注入后提前结束，Abort 必须撤销故障。
func TestOrchestratorAbortRevokes(t *testing.T) {
	exec := &fakeExecutor{}
	clock := &fakeClock{t: time.Now()}
	orch := newOrch(t, exec, delayScenario(0), time.Second, clock, service.EventHandler{})
	ctx := context.Background()
	if p, _ := orch.Step(ctx, 0, cpuMetric(95)); p != service.PhaseInjected {
		t.Fatal("应已注入")
	}
	if err := orch.Abort(ctx); err != nil {
		t.Fatalf("Abort失败: %v", err)
	}
	if orch.Phase() != service.PhaseRevoked {
		t.Fatalf("Abort后应revoked，实际 %s", orch.Phase())
	}
	last := exec.lastArgs()
	if len(last) == 0 || last[0] != "destroy" {
		t.Fatalf("Abort应执行destroy，实际 %v", last)
	}
}

// TestOrchestratorAbortBeforeInject 未注入时 Abort 安全无操作。
func TestOrchestratorAbortBeforeInject(t *testing.T) {
	exec := &fakeExecutor{}
	clock := &fakeClock{t: time.Now()}
	orch := newOrch(t, exec, delayScenario(60), time.Second, clock, service.EventHandler{})
	if err := orch.Abort(context.Background()); err != nil {
		t.Fatalf("未注入Abort应安全: %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatal("未注入不应有blade调用")
	}
}
