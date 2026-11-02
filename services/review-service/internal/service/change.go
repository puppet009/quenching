// change.go 助手修改记录章节生成器（M5）。
//
// 与坑点章节同采用规则生成（不调 AI）：修改记录是 write_file 执行时的
// 事实快照，路径、时间、前后内容全部确定，任何 LLM 重写都可能产生偏差，
// 因此以确定性章节呈现，作为故障排查的操作留痕。
//
// 输出：
//   - Markdown 正文（content 字段），供复盘页直接渲染
//   - 结构化 JSON（content_data 字段），供前端后续做 diff 卡片

package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quenching/review-service/internal/model"
)

// changeSnippetLimit 单段内容在 Markdown 正文中的最大字符数，
// 超长截断，避免大文件快照把报告撑得过长；完整内容仍在 content_data 中。
const changeSnippetLimit = 2000

// codeFence 代码围栏长度。写入内容本身可能含三反引号，
// 统一用四反引号围栏以保证 Markdown 结构不被内容打断。
const codeFence = "````"

// BuildChangeSection 从修改记录数据生成复盘章节。
//
// 无修改记录时返回 nil（调用方决定是否跳过该章节）。
func BuildChangeSection(reviewID int64, analysis *model.ChangeAnalysis) *model.ReviewSection {
	if analysis == nil || analysis.TotalCount == 0 {
		return nil
	}

	content := buildChangeMarkdown(analysis)
	contentData, _ := json.Marshal(analysis)

	return &model.ReviewSection{
		ReviewID:     reviewID,
		SectionType:  model.SectionTypeAssistantChanges,
		SectionLevel: model.ChangeSectionLevel,
		Title:        model.ChangeSectionTitle,
		Content:      content,
		ContentData:  contentData,
		SortOrder:    model.ChangeSectionOrder,
	}
}

// buildChangeMarkdown 生成修改记录 Markdown 正文。
func buildChangeMarkdown(a *model.ChangeAnalysis) string {
	var sb strings.Builder

	sb.WriteString("# " + model.ChangeSectionTitle + "\n\n")

	// 概述
	created, overwritten := 0, 0
	for _, c := range a.Changes {
		if c.BeforeExisted {
			overwritten++
		} else {
			created++
		}
	}
	sb.WriteString("## 概述\n\n")
	sb.WriteString(fmt.Sprintf(
		"实训助手在你授权下共执行了 **%d** 次文件写入：新建文件 **%d** 个，覆盖已有文件 **%d** 个。"+
			"沙箱销毁后这些改动随之清除，此处仅作操作留痕。\n\n",
		a.TotalCount, created, overwritten,
	))

	// 按 Pod 分布
	podStats := make(map[string]int)
	for _, c := range a.Changes {
		podStats[c.Pod]++
	}
	if len(podStats) > 0 {
		sb.WriteString("### 按服务分布\n\n")
		sb.WriteString("| 实例 (Pod) | 修改次数 |\n")
		sb.WriteString("|------------|----------|\n")
		for pod, n := range podStats {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", pod, n))
		}
		sb.WriteString("\n")
	}

	// 逐条明细
	sb.WriteString("## 修改明细\n\n")
	for i, c := range a.Changes {
		action := "新建文件"
		if c.BeforeExisted {
			action = "覆盖文件"
		}
		container := ""
		if c.Container != "" {
			container = fmt.Sprintf(" / 容器 `%s`", c.Container)
		}

		sb.WriteString(fmt.Sprintf("### %d. %s（%s）\n\n", i+1, action, formatOffset(c.OffsetSec)))
		sb.WriteString(fmt.Sprintf("- **实例**：`%s`%s\n", c.Pod, container))
		sb.WriteString(fmt.Sprintf("- **路径**：`%s`\n\n", c.Path))

		// 覆盖文件：先展示写入前内容
		if c.BeforeExisted {
			sb.WriteString("#### 写入前\n\n")
			sb.WriteString(codeFence + "\n")
			sb.WriteString(clipContent(c.BeforeContent) + "\n")
			sb.WriteString(codeFence + "\n\n")
		}

		// 写入后内容
		sb.WriteString("#### 写入后\n\n")
		sb.WriteString(codeFence + "\n")
		sb.WriteString(clipContent(c.AfterContent) + "\n")
		sb.WriteString(codeFence + "\n\n")
	}

	return sb.String()
}

// clipContent 截断超长内容并追加省略提示。
func clipContent(s string) string {
	if len(s) <= changeSnippetLimit {
		return s
	}
	return s[:changeSnippetLimit] +
		fmt.Sprintf("\n...（内容过长，已截断，完整内容共 %d 字符，详见结构化数据）", len(s))
}

// formatOffset 把相对会话开始的秒数格式化为「第 m分s秒」。
func formatOffset(sec int) string {
	if sec < 0 {
		sec = 0
	}
	if sec < 60 {
		return fmt.Sprintf("第 %d 秒", sec)
	}
	return fmt.Sprintf("第 %d 分 %d 秒", sec/60, sec%60)
}
