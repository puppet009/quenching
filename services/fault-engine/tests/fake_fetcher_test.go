package tests

import (
	"context"

	"github.com/quenching/fault-engine/internal/service"
)

// fakeFetcher 按调用次序返回预设样本，模拟指标随时间变化。
type fakeFetcher struct {
	frames [][]service.JudgeMetricSample
	cursor int
	err    error
}

func (f *fakeFetcher) Fetch(_ context.Context, _ string, _ []string) ([]service.JudgeMetricSample, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.cursor >= len(f.frames) {
		return f.frames[len(f.frames)-1], nil // 之后一直返回最后一帧
	}
	s := f.frames[f.cursor]
	f.cursor++
	return s, nil
}
