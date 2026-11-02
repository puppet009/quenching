package service

import (
	"context"

	"github.com/quenching/review-service/internal/model"
)

// 编译期保证 ChangeSectionBuilder 实现本层 SectionCollector 端口。
var _ SectionCollector = (*ChangeSectionBuilder)(nil)

// ChangeSource 修改记录采集端口（M5「修改操作登记+复盘」）。
// 由 repository 层实现（HTTP 调用 training-service），
// service 层不感知具体取数方式。
type ChangeSource interface {
	// CollectSessionChanges 采集会话全部助手文件修改记录，无记录返回空分析。
	CollectSessionChanges(ctx context.Context, sessionID int64) (*model.ChangeAnalysis, error)
}

// ChangeSectionBuilder 助手修改记录章节构建器。
//
// 组合 ChangeSource（数据采集）+ BuildChangeSection（规则生成），适配为
// SectionCollector 端口，供复盘 worker 在生成主章节后追加确定性修改记录章节。
type ChangeSectionBuilder struct {
	source ChangeSource
}

// NewChangeSectionBuilder 创建助手修改记录章节构建器。
func NewChangeSectionBuilder(source ChangeSource) *ChangeSectionBuilder {
	return &ChangeSectionBuilder{source: source}
}

// CollectAndBuild 采集会话修改记录并生成复盘章节。
// 实现 SectionCollector 接口。
// 无修改记录时返回 nil, nil（不生成章节）。
func (b *ChangeSectionBuilder) CollectAndBuild(ctx context.Context, reviewID, sessionID int64) (*model.ReviewSection, error) {
	if b == nil || b.source == nil {
		return nil, nil
	}

	analysis, err := b.source.CollectSessionChanges(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return BuildChangeSection(reviewID, analysis), nil
}
