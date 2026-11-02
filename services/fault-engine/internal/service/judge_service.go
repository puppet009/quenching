// 修复判定服务：周期性拉指标 -> 交给 JudgeEvaluator 评估 -> 持续达标后回调。
// 判定规则全部在本包的 JudgeEvaluator（纯逻辑、可单测）；本服务只负责调度与对接外部通道。
package service

import (
	"context"
	"time"

	"github.com/quenching/go-common/logger"
	"go.uber.org/zap"
)

// JudgeService 单场故障的判定轮询器。
type JudgeService struct {
	fetcher   JudgeMetricFetcher
	evaluator *JudgeEvaluator
	callback  JudgeResolveCallback

	namespace string
	metrics   []string
	interval  time.Duration
}

// JudgeConfig 构造参数。
type JudgeConfig struct {
	Namespace      string
	Thresholds     []JudgeThreshold
	StableRequired time.Duration // 连续达标窗口
	PollInterval   time.Duration // 轮询间隔
	OnResolved     JudgeResolveCallback
}

func NewJudgeService(fetcher JudgeMetricFetcher, cfg JudgeConfig) *JudgeService {
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	metrics := make([]string, 0, len(cfg.Thresholds))
	for _, th := range cfg.Thresholds {
		metrics = append(metrics, th.Name)
	}
	return &JudgeService{
		fetcher:   fetcher,
		evaluator: NewJudgeEvaluator(cfg.Thresholds, cfg.StableRequired),
		callback:  cfg.OnResolved,
		namespace: cfg.Namespace,
		metrics:   metrics,
		interval:  interval,
	}
}

// Run 阻塞轮询直到判定修复（回调）或 ctx 取消（实训结束/超时）。
func (s *JudgeService) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.poll(ctx) // 启动先查一轮
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.poll(ctx) {
				return
			}
		}
	}
}

// poll 执行一轮，返回 true 表示已判定修复、可停止轮询。
func (s *JudgeService) poll(ctx context.Context) bool {
	samples, err := s.fetcher.Fetch(ctx, s.namespace, s.metrics)
	if err != nil {
		logger.L().Warn("拉取判定指标失败", zap.String("ns", s.namespace), zap.Error(err))
		return false // 拉取失败不误判，下轮重试
	}
	res := s.evaluator.Evaluate(samples, time.Now())
	if res.Verdict == JudgeVerdictResolved {
		logger.L().Info("故障已修复", zap.String("ns", s.namespace))
		if s.callback != nil {
			if err := s.callback(ctx, s.namespace); err != nil {
				logger.L().Warn("修复回调失败", zap.Error(err))
				return false
			}
		}
		return true
	}
	return false
}

// Evaluator 暴露判定器给上层编排（稳定窗口评估）。
func (s *JudgeService) Evaluator() *JudgeEvaluator { return s.evaluator }
