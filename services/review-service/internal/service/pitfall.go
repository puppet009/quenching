// pitfall.go 坑点分析章节生成器（T3-13）。
//
// 采用规则生成（不调 AI），原因：
//   - 坑点配置数据结构化程度高（错误内容、正确做法、严重程度都已定义）
//   - 用户响应类型是确定的枚举值（踩/识别）
//   - 复盘生成链路不需要额外 AI 调用，性能好成本低
//   - 内容确定性高，便于测试与前端渲染对齐
//
// 输出：
//   - Markdown 正文（content 字段），供复盘页直接渲染
//   - 结构化 JSON（content_data 字段），供前端卡片/时间线渲染

package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quenching/review-service/internal/model"
)

// BuildPitfallSection 从坑点分析数据生成复盘章节。
//
// 无坑点时返回 nil（调用方决定是否跳过该章节）。
func BuildPitfallSection(reviewID int64, analysis *model.PitfallAnalysis) *model.ReviewSection {
	if analysis == nil || analysis.TotalCount == 0 {
		return nil
	}

	content := buildPitfallMarkdown(analysis)
	contentData, _ := json.Marshal(analysis)

	return &model.ReviewSection{
		ReviewID:     reviewID,
		SectionType:  model.SectionTypeFixAIPitfallAnalysis,
		SectionLevel: model.PitfallSectionLevel,
		Title:        model.PitfallSectionTitle,
		Content:      content,
		ContentData:  contentData,
		SortOrder:    model.PitfallSectionOrder,
	}
}

// buildPitfallMarkdown 生成坑点分析 Markdown 正文。
func buildPitfallMarkdown(a *model.PitfallAnalysis) string {
	var sb strings.Builder

	// 章节大标题（与 PitfallSectionTitle 对齐，前端也可直接渲染 Markdown）
	sb.WriteString("# " + model.PitfallSectionTitle + "\n\n")

	// 概述
	sb.WriteString("## 概述\n\n")
	sb.WriteString(fmt.Sprintf(
		"本次训练共出现 **%d** 个 AI 坑点，你成功识别了 **%d** 个，识别率为 **%.1f%%**。\n\n",
		a.TotalCount, a.RecognizedCount, a.RecognitionRate,
	))

	// 按类型分组统计
	typeStats := make(map[string]int)
	typeRecognized := make(map[string]int)
	for _, p := range a.Pitfalls {
		typeStats[p.PitfallType]++
		if p.Recognized {
			typeRecognized[p.PitfallType]++
		}
	}
	if len(typeStats) > 0 {
		sb.WriteString("### 按类型分布\n\n")
		sb.WriteString("| 坑点类型 | 出现次数 | 识别数 | 识别率 |\n")
		sb.WriteString("|----------|----------|--------|--------|\n")
		for t, count := range typeStats {
			recog := typeRecognized[t]
			rate := 0.0
			if count > 0 {
				rate = float64(recog) / float64(count) * 100
			}
			sb.WriteString(fmt.Sprintf("| %s | %d | %d | %.1f%% |\n",
				model.PitfallTypeLabel(t), count, recog, rate))
		}
		sb.WriteString("\n")
	}

	// 逐坑点分析
	sb.WriteString("## 坑点详情\n\n")

	for i, p := range a.Pitfalls {
		sb.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, p.Name))

		// 基本信息
		sb.WriteString(fmt.Sprintf("- **类型**：%s\n", model.PitfallTypeLabel(p.PitfallType)))
		sb.WriteString(fmt.Sprintf("- **严重程度**：%s\n", model.SeverityLabel(p.Severity)))
		sb.WriteString(fmt.Sprintf("- **出现时间**：第 %d 秒\n", p.AppearOffsetSec))

		// 你的反应
		statusIcon := "✅"
		if !p.Recognized {
			statusIcon = "⚠️"
		}
		sb.WriteString(fmt.Sprintf("- **你的反应**：%s %s\n\n", statusIcon, model.ResponseLabel(p.UserResponse)))

		// AI 说了什么（错误建议）
		if p.ErrorContent != "" {
			sb.WriteString("#### AI 的错误建议\n\n")
			// 截取前 300 字，避免过长
			errContent := p.ErrorContent
			if len(errContent) > 300 {
				errContent = errContent[:300] + "..."
			}
			sb.WriteString("> " + strings.ReplaceAll(errContent, "\n", "\n> ") + "\n\n")
		}

		// 为什么这是个坑
		if p.Description != "" {
			sb.WriteString("#### 为什么这是个坑\n\n")
			sb.WriteString(p.Description + "\n\n")
		}

		// 正确识别方式
		if p.CorrectApproach != "" {
			sb.WriteString("#### 正确的做法\n\n")
			sb.WriteString(p.CorrectApproach + "\n\n")
		}

		// 改进建议
		sb.WriteString("#### 改进建议\n\n")
		sb.WriteString(buildImprovementSuggestion(p) + "\n\n")
	}

	// 总体改进建议
	sb.WriteString("## 总体建议\n\n")
	sb.WriteString(buildOverallSuggestions(a))

	return sb.String()
}

// buildImprovementSuggestion 为单个坑点生成改进建议。
func buildImprovementSuggestion(p model.PitfallItem) string {
	if p.Recognized {
		// 识别了坑点 -> 肯定 + 提醒保持
		switch p.UserResponse {
		case model.PitfallResponseDisputed:
			return "你对 AI 的建议提出了质疑，这是很好的习惯。继续保持批判性思维，在采纳建议前先验证其合理性。"
		case model.PitfallResponseVerified:
			return "你通过验证后纠正了方向，这是最稳妥的做法。建议在面对关键决策时，始终坚持'先验证、再执行'的原则。"
		case model.PitfallResponseIgnored:
			return "你忽略了这个错误建议，说明你的判断力很强。但也要注意：忽略不等于确认，建议在条件允许时验证一下直觉。"
		default:
			return "你成功识别了这个坑点，做得很好！继续保持对 AI 输出的审慎态度。"
		}
	}

	// 踩了坑 -> 针对性建议
	switch p.PitfallType {
	case model.PitfallTypeCausalMisjudgment:
		return "这是一个因果误判类坑点。建议在排查问题时：(1) 不要急于下结论；(2) 用数据和实验验证因果关系；(3) 考虑多种可能性，逐一排除。"
	case model.PitfallTypeInfoOmission:
		return "这是一个信息遗漏类坑点。AI 可能有意或无意地遗漏了关键信息。建议：(1) 多问几个'为什么'；(2) 主动补充上下文；(3) 交叉验证信息来源。"
	case model.PitfallTypeOverconfidence:
		return "这是一个过度自信类坑点。AI 有时会给出看似笃定但实际错误的答案。建议：(1) 对高置信度的说法保持警惕；(2) 要求给出依据和推理过程；(3) 关键结论必须验证。"
	default:
		return "建议在下次遇到类似建议时，先停下来思考：这个建议的依据是什么？有没有可能遗漏了什么？多一步验证，少一步踩坑。"
	}
}

// buildOverallSuggestions 生成总体改进建议。
func buildOverallSuggestions(a *model.PitfallAnalysis) string {
	if a.TotalCount == 0 {
		return "本次训练未检测到 AI 坑点。"
	}

	rate := a.RecognitionRate
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("本次训练你遇到了 %d 个 AI 坑点，识别率 %.1f%%。", a.TotalCount, rate))

	if rate >= 80 {
		sb.WriteString("**表现优秀！** 你对 AI 输出有很强的判断力，大部分坑点都被你成功识别。继续保持这种审慎的态度。\n\n")
		sb.WriteString("进阶建议：\n")
		sb.WriteString("- 尝试总结你识别坑点的思路，形成自己的'排雷方法论'\n")
		sb.WriteString("- 关注那些险些踩坑的时刻，它们往往是能力边界所在\n")
	} else if rate >= 50 {
		sb.WriteString("**表现尚可。** 你识别了超过半数的坑点，但仍有提升空间。\n\n")
		sb.WriteString("改进建议：\n")
		sb.WriteString("- 对 AI 给出的'笃定'结论要特别警惕，过度自信是常见坑点类型\n")
		sb.WriteString("- 养成'先验证、再执行'的习惯，尤其是在做关键操作前\n")
		sb.WriteString("- 多问'为什么'，让 AI 解释推理过程，而不是只给结论\n")
	} else {
		sb.WriteString("**需要加强。** 大部分坑点你都没有识别出来，这在真实工作场景中可能导致严重后果。\n\n")
		sb.WriteString("重点改进方向：\n")
		sb.WriteString("- 建立对 AI 输出的'默认怀疑'心态，不轻易下结论\n")
		sb.WriteString("- 学习常见的坑点类型（因果误判、信息遗漏、过度自信），形成条件反射\n")
		sb.WriteString("- 每次采纳 AI 建议前，问自己三个问题：依据是什么？有没有遗漏？能验证吗？\n")
		sb.WriteString("- 建议回顾未识别的坑点，仔细体会它们的迷惑性在哪里\n")
	}

	// 按表现分类的针对性建议
	unrecognized := 0
	for _, p := range a.Pitfalls {
		if !p.Recognized {
			unrecognized++
		}
	}
	if unrecognized > 0 {
		sb.WriteString(fmt.Sprintf("\n你有 %d 个坑点未能识别，建议重点复盘这些场景，加深对类似陷阱的免疫力。", unrecognized))
	}

	return sb.String()
}
