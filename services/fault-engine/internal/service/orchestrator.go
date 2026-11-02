// Package service 的 orchestrator.go：故障编排服务。
// 把四块串成一条状态机：
//
//	加载剧本 -> 等待触发(scheduler) -> 注入(injector) -> 排查中轮询判定(judge)
//	         -> 判定修复 -> 自动撤销注入(injector.Revoke)
//
// 设计要点：
//   - 不依赖定时器/真实集群，每一步由上层用 Step(ctx, elapsed, metrics) 显式驱动，
//     时间与指标都从外部传入，因此整条生命周期可在无集群环境下单测。
//   - 只有编排器持有故障的整体阶段状态；scheduler/injector/judge 各管一段。
//   - 用户提前结束实训时调 Abort，保证已注入的故障一定被撤销（不泄漏）。
package service

import (
	"context"
	"sync"
	"time"

	"github.com/quenching/fault-engine/internal/model"
)

// Phase 故障编排阶段。
type Phase string

const (
	PhaseArmed    Phase = "armed"    // 已武装，等待触发
	PhaseInjected Phase = "injected" // 已注入，等待用户修复
	PhaseResolved Phase = "resolved" // 判定已修复
	PhaseRevoked  Phase = "revoked"  // 注入已撤销（终态）
	PhaseSkipped  Phase = "skipped"  // 触发窗口错过，未注入（终态）
	PhaseFailed   Phase = "failed"   // 注入/恢复失败（终态，需告警）
)

// EventHandler 编排过程中的关键事件回调（上层发领域事件/推送前端）。
// 全部可选。
type EventHandler struct {
	OnInjected func(ctx context.Context, exp *model.Experiment)
	OnResolved func(ctx context.Context, sessionID int64)
	OnRevoked  func(ctx context.Context, sessionID int64)
	OnFailed   func(ctx context.Context, sessionID int64, err error)
}

// Clock 可注入时钟（生产用 time.Now，测试用固定时间），与 judge 稳定窗口配合。
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Orchestrator 单场故障的编排器（一个训练会话一个实例）。
type Orchestrator struct {
	scn     *Scenario
	trigger *TriggerState
	inj     *Injector
	judge   *JudgeEvaluator
	events  EventHandler
	clock   Clock

	sessionID int64
	namespace string

	stateMu sync.RWMutex // 保护查询接口与驱动循环共享的阶段和实验信息。
	phase   Phase
	exp     *model.Experiment // 注入成功后保存，恢复时用其 uid
}

// OrchestratorConfig 构造参数。
type OrchestratorConfig struct {
	SessionID      int64
	Namespace      string
	Scenario       *Scenario
	Injector       *Injector
	StableRequired time.Duration // 修复判定稳定窗口
	Clock          Clock         // 可选，默认系统时钟
	Events         EventHandler
}

func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	thresholds := make([]JudgeThreshold, 0, len(cfg.Scenario.Recovery))
	for _, r := range cfg.Scenario.Recovery {
		thresholds = append(thresholds, JudgeThreshold{
			Name: r.Metric, Service: r.Service,
			Op: JudgeOperator(r.Op), Value: r.Value,
		})
	}
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Orchestrator{
		scn:       cfg.Scenario,
		trigger:   NewTriggerState(cfg.Scenario),
		inj:       cfg.Injector,
		judge:     NewJudgeEvaluator(thresholds, cfg.StableRequired),
		events:    cfg.Events,
		clock:     clock,
		sessionID: cfg.SessionID,
		namespace: cfg.Namespace,
		phase:     PhaseArmed,
	}
}

// Phase 返回当前阶段。
func (o *Orchestrator) Phase() Phase {
	o.stateMu.RLock()
	defer o.stateMu.RUnlock()
	return o.phase
}

func (o *Orchestrator) setPhase(phase Phase) {
	o.stateMu.Lock()
	o.phase = phase
	o.stateMu.Unlock()
}

// ExperimentUID 返回已注入实验的 uid（未注入为空）。
func (o *Orchestrator) ExperimentUID() string {
	o.stateMu.RLock()
	defer o.stateMu.RUnlock()
	if o.exp != nil {
		return o.exp.ExperimentUID
	}
	return ""
}

// Step 推进一步。elapsedSeconds 为实训已进行秒数，metrics 为当前指标采样。
// 上层每个轮询周期调用一次。返回当前阶段。
// Step 与 Abort 由同一个驱动循环串行调用；Phase/ExperimentUID 可并发查询。
func (o *Orchestrator) Step(ctx context.Context, elapsedSeconds int, metrics []JudgeMetricSample) (Phase, error) {
	switch o.phase {
	case PhaseArmed:
		return o.handleArmed(ctx, elapsedSeconds, metrics)
	case PhaseInjected:
		return o.handleInjected(ctx, metrics)
	default:
		// resolved/revoked/skipped/failed 终态不再推进
		return o.phase, nil
	}
}

// handleArmed 武装阶段：问调度器该不该触发。
func (o *Orchestrator) handleArmed(ctx context.Context, elapsed int, metrics []JudgeMetricSample) (Phase, error) {
	decision := o.trigger.Evaluate(TriggerRuntimeContext{ElapsedSeconds: elapsed, Metrics: metrics})
	switch decision {
	case TriggerDecisionFire:
		return o.inject(ctx)
	case TriggerDecisionExpired:
		o.setPhase(PhaseSkipped)
		return o.phase, nil
	default:
		return o.phase, nil // Wait，继续等
	}
}

// inject 执行真实注入。
func (o *Orchestrator) inject(ctx context.Context) (Phase, error) {
	spec := o.buildSpec()
	exp, err := o.inj.Inject(ctx, spec)
	if err != nil {
		o.setPhase(PhaseFailed)
		o.emitFailed(ctx, err)
		return o.phase, err
	}
	o.stateMu.Lock()
	o.exp = exp
	o.phase = PhaseInjected
	o.stateMu.Unlock()
	o.trigger.MarkFired()
	if o.events.OnInjected != nil {
		o.events.OnInjected(ctx, exp)
	}
	return o.phase, nil
}

// handleInjected 已注入阶段：用判定器看是否修复。
func (o *Orchestrator) handleInjected(ctx context.Context, metrics []JudgeMetricSample) (Phase, error) {
	res := o.judge.Evaluate(metrics, o.clock.Now())
	if res.Verdict != JudgeVerdictResolved {
		return o.phase, nil
	}
	o.setPhase(PhaseResolved)
	if o.events.OnResolved != nil {
		o.events.OnResolved(ctx, o.sessionID)
	}
	// 判定修复后自动撤销注入。
	if err := o.revoke(ctx); err != nil {
		return o.phase, err
	}
	return o.phase, nil
}

// Abort 用户提前结束/实训超时时调用：若已注入则强制撤销，保证不残留故障。
// 幂等：未注入或已撤销时安全返回。
func (o *Orchestrator) Abort(ctx context.Context) error {
	if o.phase != PhaseInjected && o.phase != PhaseResolved {
		return nil
	}
	if o.exp == nil {
		o.setPhase(PhaseRevoked)
		return nil
	}
	return o.revoke(ctx)
}

func (o *Orchestrator) revoke(ctx context.Context) error {
	if o.exp == nil {
		return nil
	}
	if err := o.inj.Revoke(ctx, o.buildSpec(), o.exp.ExperimentUID); err != nil {
		o.setPhase(PhaseFailed)
		o.emitFailed(ctx, err)
		return err
	}
	o.setPhase(PhaseRevoked)
	if o.events.OnRevoked != nil {
		o.events.OnRevoked(ctx, o.sessionID)
	}
	return nil
}

// buildSpec 把剧本配置 + 运行时 namespace 组装成注入规格。
func (o *Orchestrator) buildSpec() model.InjectSpec {
	spec := o.scn.Spec
	spec.Kind = o.scn.Kind
	spec.TargetNamespace = o.namespace
	spec.TargetPod = o.scn.Target.Pod
	spec.TargetProcess = o.scn.Target.Process
	if spec.NetworkInterface == "" {
		spec.NetworkInterface = o.scn.Target.NetworkInterface
	}
	return spec
}

func (o *Orchestrator) emitFailed(ctx context.Context, err error) {
	if o.events.OnFailed != nil {
		o.events.OnFailed(ctx, o.sessionID, err)
	}
}
