package service

import "context"

// JudgeMetricFetcher 指标获取通道抽象。
//   - 生产：JudgePromFetcher 用 PromQL 查 Prometheus（云上补实现）。
//   - 测试：fakeFetcher 按时间返回预设样本，不依赖集群。
type JudgeMetricFetcher interface {
	// Fetch 拉取本次故障相关指标的当前值。
	Fetch(ctx context.Context, namespace string, metrics []string) ([]JudgeMetricSample, error)
}

// JudgeResolveCallback 判定为已修复时的回调（上层用于推送 score_update、结束实训）。
type JudgeResolveCallback func(ctx context.Context, namespace string) error
