package service

import (
	"context"

	"github.com/quenching/review-service/internal/model"
)

// 编译期保证 PitfallSectionBuilder 实现本层 SectionCollector 端口。
var _ SectionCollector = (*PitfallSectionBuilder)(nil)

// PitfallSource 坑点数据采集端口（架构拆库第五批）。
// 由 repository 层实现：拆库前为 PG 直查，拆库后为 HTTP 调用 training-service，
// service 层不感知具体取数方式。
type PitfallSource interface {
	// CollectSessionPitfalls 采集会话全部坑点（含识别统计），无坑点返回空分析。
	CollectSessionPitfalls(ctx context.Context, sessionID int64) (*model.PitfallAnalysis, error)
}

// PitfallSectionBuilder 坑点章节构建器（T3-13）。
//
// 组合 PitfallSource（数据采集）+ BuildPitfallSection（规则生成），适配为
// SectionCollector 端口，供复盘 worker 在生成主章节后追加坑点分析章节。
type PitfallSectionBuilder struct {
	source PitfallSource
}

// NewPitfallSectionBuilder 创建坑点章节构建器。
func NewPitfallSectionBuilder(source PitfallSource) *PitfallSectionBuilder {
	return &PitfallSectionBuilder{source: source}
}

// CollectAndBuild 采集会话坑点数据并生成复盘章节。
// 实现 SectionCollector 接口。
// 无坑点时返回 nil, nil（不生成章节）。
func (b *PitfallSectionBuilder) CollectAndBuild(ctx context.Context, reviewID, sessionID int64) (*model.ReviewSection, error) {
	if b == nil || b.source == nil {
		return nil, nil
	}

	analysis, err := b.source.CollectSessionPitfalls(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	section := BuildPitfallSection(reviewID, analysis)
	return section, nil
}
