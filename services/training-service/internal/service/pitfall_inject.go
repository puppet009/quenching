package service

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/quenching/training-service/internal/model"
)

// Injection 一次命中后要注入给用户的确定性助手消息（T3-9 交付核心）。
//
// 设计取舍：MVP 规则版坑点"稳定触发、100% 出现"，因此错误建议不经过 LLM 再生成，
// 而是由配置的 ErrorContent（错误建议模板）确定性直出，从根上保证"符合条件必出现"。
// 置信度按配置档位包装（random 在本会话内随机选低/中/高），避免用户学到
// "越自信越是坑"的规律（技术架构 §11.6）。
type Injection struct {
	ConfigID        int64
	DefectID        int64
	PitfallType     string
	ConfidenceLevel string
	// Content 最终下发并落库的 assistant 消息正文（markdown）。
	Content string
}

// confidenceRoller 置信度随机分布的随机源（与触发概率的 RollSource 分离，语义独立）。
type confidenceRoller interface {
	Intn(n int) int
}

// BuildInjection 根据命中的配置构造确定性注入消息。
// confRoll 传 nil 时用默认 math/rand；测试可注入固定源。
func BuildInjection(c *model.PitfallConfig, confRoll confidenceRoller) *Injection {
	if confRoll == nil {
		confRoll = rand.New(rand.NewSource(rand.Int63()))
	}
	level := resolveConfidence(c.ConfidenceLevel, confRoll)

	return &Injection{
		ConfigID:        c.ID,
		DefectID:        c.DefectID,
		PitfallType:     c.PitfallType,
		ConfidenceLevel: level,
		Content:         renderContent(c, level, confRoll),
	}
}

// resolveConfidence 解析实际呈现的置信度档位；random 在低/中/高里均匀取一档。
func resolveConfidence(configured string, roll confidenceRoller) string {
	switch configured {
	case model.ConfidenceLow, model.ConfidenceMedium, model.ConfidenceHigh:
		return configured
	default: // random 或缺省
		return []string{model.ConfidenceLow, model.ConfidenceMedium, model.ConfidenceHigh}[roll.Intn(3)]
	}
}

// renderContent 组装最终话术：置信度开场 + 配置的错误建议正文 + 可选结尾。
// ErrorContent 已是运营在后台配置好的完整错误建议，模板只负责确定性包装，不改写其实质内容。
func renderContent(c *model.PitfallConfig, level string, roll confidenceRoller) string {
	var sb strings.Builder
	sb.WriteString(pickOpening(level, roll))
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(c.ErrorContent))

	// 可选结尾话术（按档位概率出现）
	if closing := pickClosing(level, roll); closing != "" {
		sb.WriteString("\n\n")
		sb.WriteString(closing)
	}
	return sb.String()
}

// String 便于日志排查。
func (in *Injection) String() string {
	return fmt.Sprintf("injection(config=%d defect=%d type=%s confidence=%s)",
		in.ConfigID, in.DefectID, in.PitfallType, in.ConfidenceLevel)
}
