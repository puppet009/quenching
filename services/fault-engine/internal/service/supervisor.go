// supervisor.go：FaultSupervisor 单会话故障驱动循环。
//
// 职责（云上长期运行的部分）：
//   - 周期性拉取沙箱指标（JudgeMetricFetcher）
//   - 计算实训已进行秒数，驱动 Orchestrator.Step
//   - 编排器进入终态（revoked/skipped/failed）后自动停止循环
//   - 外部停止（实训结束/超时）时调 Abort，保证故障被撤销
//
// 指标获取、时钟均可注入，因此驱动逻辑在无集群下可用 fake 完整单测；
// 真正的 Prometheus 拉取与 RabbitMQ 事件发布在云上接线实现。
package service

import (
	"context"
	"time"

	"github.com/quenching/go-common/logger"
	"go.uber.org/zap"
)

// defaultPollInterval 驱动周期，与判定轮询一致。
const defaultPollInterval = 5 * time.Second

// FaultSupervisor 驱动单个训练会话的故障编排。
type FaultSupervisor struct {
	orch     *Orchestrator
	fetcher  JudgeMetricFetcher
	clock    Clock
	interval time.Duration

	namespace string
	startedAt time.Time
	metrics   []string // 需要拉取的指标名

	// stopCh 外部停止信号（实训结束/超时）。
	stopCh chan struct{}
	doneCh chan struct{}
}

// SupervisorConfig 构造参数。
type SupervisorConfig struct {
	Orchestrator *Orchestrator
	Fetcher      JudgeMetricFetcher
	Clock        Clock
	Interval     time.Duration
	Namespace    string
	StartedAt    time.Time
	Metrics      []string
}

func NewFaultSupervisor(cfg SupervisorConfig) *FaultSupervisor {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	started := cfg.StartedAt
	if started.IsZero() {
		started = clock.Now()
	}
	return &FaultSupervisor{
		orch:      cfg.Orchestrator,
		fetcher:   cfg.Fetcher,
		clock:     clock,
		interval:  interval,
		namespace: cfg.Namespace,
		startedAt: started,
		metrics:   cfg.Metrics,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Run 阻塞运行驱动循环，直到：编排器进入终态、ctx 取消、或调用 Stop。
func (s *FaultSupervisor) Run(ctx context.Context) {
	defer close(s.doneCh)

	// 启动先跑一轮（支持 delay=0 立即注入）。
	if s.Tick(ctx) {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.shutdown(context.Background())
			return
		case <-s.stopCh:
			s.shutdown(context.Background())
			return
		case <-ticker.C:
			if s.Tick(ctx) {
				return
			}
		}
	}
}

// Tick 执行一轮驱动（拉指标 -> 算已进行秒数 -> 推进编排器）。
// 返回 true 表示编排器已进入终态，应停止循环。公开以便上层/测试直接驱动。
func (s *FaultSupervisor) Tick(ctx context.Context) bool {
	elapsed := int(s.clock.Now().Sub(s.startedAt).Seconds())

	samples, err := s.fetcher.Fetch(ctx, s.namespace, s.metrics)
	if err != nil {
		// 拉指标失败：跳过本轮（编排器不会因缺数据误判），下轮重试。
		logger.L().Warn("supervisor 拉取指标失败", zap.String("ns", s.namespace), zap.Error(err))
		return false
	}

	phase, err := s.orch.Step(ctx, elapsed, samples)
	if err != nil {
		logger.L().Error("supervisor 驱动故障编排失败", zap.String("ns", s.namespace), zap.Error(err))
		return phase == PhaseFailed
	}

	switch phase {
	case PhaseRevoked, PhaseSkipped, PhaseFailed:
		logger.L().Info("故障编排终态，停止驱动", zap.String("ns", s.namespace), zap.String("phase", string(phase)))
		return true
	}
	return false
}

// shutdown 外部停止：调 Abort 撤销可能已注入的故障（幂等）。
func (s *FaultSupervisor) shutdown(ctx context.Context) {
	if err := s.orch.Abort(ctx); err != nil {
		logger.L().Error("停止时撤销故障失败", zap.String("ns", s.namespace), zap.Error(err))
	}
}

// Stop 请求停止循环（非阻塞），可等待 Done 确认退出。
func (s *FaultSupervisor) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

// Done 返回循环退出信号。
func (s *FaultSupervisor) Done() <-chan struct{} { return s.doneCh }

// Phase 透传当前编排阶段。
func (s *FaultSupervisor) Phase() Phase { return s.orch.Phase() }
