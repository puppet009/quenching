package service

import (
	"context"
	"sync"

	"github.com/quenching/review-service/internal/model"
)

// MethodReviewGenerator 方法论复盘生成器（T3-1 路径对比 / T3-2 六章），
// 实现 worker.ReviewGenerator 与扩展端口 worker.MethodSectionsGenerator。
//
// 与 BasicReviewGenerator 的差异：
//   - 使用方法级指令（BuildMethodPrompt，method-v3，T3-3 校准），输出六章；
//   - 按故障类型经 templates Provider 取复盘模板（generic 兜底），把要点注入指令；
//   - 用 ParseMethodSections 做强校验（六章必填）；
//   - 额外实现 MethodSections()，供 worker 取出七返回值无法承载的章节。
//
// 七返回值签名被 T2-31 锁定、无法增加章节返回槽位，因此六章结果在
// Generate 成功后缓存于本结构，worker 紧接着通过 MethodSections() 取走。
// 调用契约：worker 串行消费（Qos prefetchCount=1），每次成功 Generate 后
// 立即读取；并发/重入安全由 mu 保证。失败的 Generate 不写入缓存（置空），
// 故重试不会读到上一次的残留章节。
type MethodReviewGenerator struct {
	inputs    InputProvider
	ai        ReviewAIClient
	templates Provider

	mu   sync.Mutex
	last *model.ReviewSections // 最近一次成功生成的六章；失败时为 nil
}

// 编译期保证满足本层的两个生成器端口。
var (
	_ ReviewGenerator         = (*MethodReviewGenerator)(nil)
	_ MethodSectionsGenerator = (*MethodReviewGenerator)(nil)
)

// NewMethodReviewGenerator 创建方法论复盘生成器。
// templates 为按故障类型供应复盘模板的端口（T3-2），调用方一般传
// NewStaticProvider()；传 nil 时内部使用静态注册表兜底。
func NewMethodReviewGenerator(inputs InputProvider, ai ReviewAIClient, templates Provider) *MethodReviewGenerator {
	if templates == nil {
		templates = NewStaticProvider()
	}
	return &MethodReviewGenerator{inputs: inputs, ai: ai, templates: templates}
}

// PromptVersion 暴露方法级 Prompt 版本，供装配层写入 reviews.prompt_version。
func (g *MethodReviewGenerator) PromptVersion() string {
	return MethodPromptVersion
}

// Generate 实现 ReviewGenerator。
// summary=根因分析、overall=时间线解读（与基础版一致地回填 reviews 两列）；
// 路径对比不进返回值，经 MethodSections() 单独取给消费者落 review_sections。
func (g *MethodReviewGenerator) Generate(ctx context.Context, task *ReviewTask) (
	title, summary, overall string, techScore, collabScore float64, durationMs int, err error,
) {
	g.resetSections()
	start := nowFunc()

	in, err := g.inputs.LoadReviewInput(ctx, task.SessionID)
	if err != nil {
		return "", "", "", 0, 0, 0, &InputError{Cause: err}
	}

	tpl := g.templates.TemplateByFaultType(in.FaultType)
	prompt := BuildMethodPrompt(in, tpl)
	sectionsJSON, err := g.ai.GenerateReview(ctx, task.SessionID, prompt)
	if err != nil {
		return "", "", "", 0, 0, 0, &AICallError{Cause: err}
	}

	sections, err := ParseMethodSections(sectionsJSON)
	if err != nil {
		return "", "", "", 0, 0, 0, err
	}

	if err = appendBrainClaims(sectionsJSON, in.Brain, sections); err != nil {
		return "", "", "", 0, 0, 0, err
	}
	durationMs = int(nowFunc().Sub(start).Milliseconds())
	g.storeSections(sections)
	return buildTitle(in), sections.RootCause, sections.Timeline, 0, 0, durationMs, nil
}

// MethodSections 返回最近一次成功 Generate 解析出的六章。
// 尚未成功生成或上一次失败时返回 nil。worker 在 Generate 成功后立即调用。
func (g *MethodReviewGenerator) MethodSections() *model.ReviewSections {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.last
}

func (g *MethodReviewGenerator) storeSections(s *model.ReviewSections) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last = s
}

func (g *MethodReviewGenerator) resetSections() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last = nil
}
