package service

import (
	"context"

	"github.com/quenching/review-service/internal/model"
)

// InputProvider 复盘输入取数端口。
// 生产实现跨服务只读查询共库（session_operations / session_defects / defects /
// training_sessions），单测用内存 fake。指标快照未落库前返回空切片、不报错。
type InputProvider interface {
	// LoadReviewInput 按会话聚合时间线、故障剧本与指标快照。
	LoadReviewInput(ctx context.Context, sessionID int64) (*model.ReviewInput, error)
}

// ReviewAIClient ai-service 复盘生成端口。
// 生产实现是 internal/ai 的 gRPC 客户端（契约 github.com/quenching/proto/aiv1）。
type ReviewAIClient interface {
	// GenerateReview 传入组装好的 Prompt，返回 sections_json 原文。
	GenerateReview(ctx context.Context, sessionID int64, p Prompt) (sectionsJSON string, err error)
}

// BasicReviewGenerator 基础复盘生成器，实现 worker.ReviewGenerator。
//
// 编排：取数 → 组装 Prompt → 调 ai-service → 解析根因/时间线两章
// → 映射为 worker 回写所需的标题/摘要/总体评价。
//
// 返回值中的 summary=根因分析、overall=时间线解读：既回填 reviews 的
// summary/overall_evaluation 列，也由 worker 映射为 review_sections 两章
// （T2-30，映射规则见 model.BasicSections）。
// 技术分/协作分由评分引擎另行计算（不在 T2-29/T2-30 范围），固定返回 0。
type BasicReviewGenerator struct {
	inputs InputProvider
	ai     ReviewAIClient
}

// 编译期保证实现 ReviewGenerator。
var _ ReviewGenerator = (*BasicReviewGenerator)(nil)

// NewBasicReviewGenerator 创建基础复盘生成器。
func NewBasicReviewGenerator(inputs InputProvider, ai ReviewAIClient) *BasicReviewGenerator {
	return &BasicReviewGenerator{inputs: inputs, ai: ai}
}

// PromptVersion 暴露当前 Prompt 版本，供装配层写入 reviews.prompt_version。
func (g *BasicReviewGenerator) PromptVersion() string {
	return PromptVersion
}

// Generate 实现 ReviewGenerator。
func (g *BasicReviewGenerator) Generate(ctx context.Context, task *ReviewTask) (
	title, summary, overall string, techScore, collabScore float64, durationMs int, err error,
) {
	start := nowFunc()

	in, err := g.inputs.LoadReviewInput(ctx, task.SessionID)
	if err != nil {
		return "", "", "", 0, 0, 0, &InputError{Cause: err}
	}

	prompt := BuildPrompt(in)
	sectionsJSON, err := g.ai.GenerateReview(ctx, task.SessionID, prompt)
	if err != nil {
		return "", "", "", 0, 0, 0, &AICallError{Cause: err}
	}

	sections, err := ParseSections(sectionsJSON)
	if err != nil {
		return "", "", "", 0, 0, 0, err
	}

	durationMs = int(nowFunc().Sub(start).Milliseconds())
	return buildTitle(in), sections.RootCause, sections.Timeline, 0, 0, durationMs, nil
}

// buildTitle 复盘标题：优先取首个剧本名称，否则用故障类型兜底。
func buildTitle(in *model.ReviewInput) string {
	if in != nil {
		for _, s := range in.Scripts {
			if s.Name != "" {
				return s.Name + " 复盘"
			}
		}
		if in.FaultType != "" {
			return in.FaultType + " 故障复盘"
		}
	}
	return "故障演练复盘"
}
