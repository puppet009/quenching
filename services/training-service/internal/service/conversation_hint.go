// T3-5 渐进式提示编排：配额限制 + 逐级解锁。
//
// 规则（难度配额取自 internal/model 的难度预设）：
//   - 简单：8 次提示，最高 3 级；普通：5 次，最高 2 级；困难：3 次，最高 1 级。
//   - 逐级解锁：首次只能请求第 1 级；此后请求级别不得超过"已用最高级别 + 1"，
//     即不允许跳过中间级别直接要更深的提示。
//   - 次数与级别均按训练会话、跨对话统计（从 ai_messages.is_hint 实时重建）。
//   - 第 3 级仍是"提示"而非答案：级别话术由 ai-service 约束（提问式收尾、不宣布根因），
//     T3-4 的根因过滤器对提示成稿同样生效。
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/quenching/training-service/internal/model"
)

var (
	// ErrHintUnavailable 非训练对话或提示能力未装配时请求提示。
	ErrHintUnavailable  = errors.New("当前对话不支持提示")
	ErrHintLevelInvalid = errors.New("提示级别非法")
	ErrHintLevelLocked  = errors.New("需要先使用上一级提示，才能解锁这一级")
	ErrHintExhausted    = errors.New("提示次数已用完")
)

// ReplyOption StreamReply 的可选参数（保持既有 4 参调用点零改动）。
type ReplyOption func(*replyOptions)

type replyOptions struct {
	hintLevel int // 0=普通应答；1/2/3=对应级别的提示
}

// WithHintLevel 请求一次指定级别的渐进式提示（1/2/3）。
func WithHintLevel(level int) ReplyOption {
	return func(o *replyOptions) { o.hintLevel = level }
}

// EnableHints 装配渐进式提示配额供给（T3-5）。provider 为 nil 时不启用，
// 训练对话收到提示请求会收到"不支持提示"错误帧，普通应答不受影响。
func (s *ChatService) EnableHints(provider HintQuotaProvider) {
	if provider == nil {
		return
	}
	s.hintQuota = provider
}

// checkHint 在调起 AI 前校验提示请求，返回本轮应下发给 ai-service 的级别与剩余次数。
// hintLevel=0 表示普通应答，直接放行、不消耗配额。
//
// fail-closed：配额/用量/难度任一读取失败都拒绝本轮提示（返回错误），
// 宁可少给一次提示，也不在计数不可信时超额泄露线索；不影响下一轮普通问答。
func (s *ChatService) checkHint(
	ctx context.Context, sessionID int64, hintLevel int,
) (level int, remaining int, err error) {
	if hintLevel == 0 {
		return 0, -1, nil
	}
	if sessionID == 0 {
		return 0, 0, ErrHintUnavailable
	}
	if s.hintQuota == nil {
		return 0, 0, ErrHintUnavailable
	}
	if hintLevel < 1 || hintLevel > 3 {
		return 0, 0, ErrHintLevelInvalid
	}

	diff, err := s.hintQuota.Difficulty(ctx, sessionID)
	if err != nil {
		return 0, 0, fmt.Errorf("读取会话难度失败: %w", err)
	}
	cfg := model.MustGet(normalizeDifficulty(diff))

	usage, err := s.hintQuota.Usage(ctx, sessionID)
	if err != nil {
		return 0, 0, fmt.Errorf("读取提示用量失败: %w", err)
	}

	if hintLevel > cfg.ProgressiveHintLevels {
		return 0, 0, fmt.Errorf("%w：当前难度最高只支持 %d 级提示",
			ErrHintLevelLocked, cfg.ProgressiveHintLevels)
	}
	if usage.Used >= cfg.HintCount {
		return 0, 0, ErrHintExhausted
	}
	// 逐级解锁：首次（MaxLevel=0）只能要 1 级；之后最多要 MaxLevel+1 级。
	if hintLevel > usage.MaxLevel+1 {
		return 0, 0, ErrHintLevelLocked
	}

	if s.brainHints != nil {
		remaining, enabled, e := s.brainHints.ReserveBrainHint(ctx, sessionID, hintLevel)
		if e != nil {
			return 0, 0, e
		}
		if enabled {
			return hintLevel, remaining, nil
		}
	}
	return hintLevel, cfg.HintCount - usage.Used - 1, nil
}

// normalizeDifficulty 把库里的难度取值归一到 model 包难度常量。
// training_sessions.difficulty 历史默认值是 'medium'，难度模型只认 easy/normal/hard，
// 这里显式映射 medium -> normal；其余未知值交由 model.MustGet 回退普通。
func normalizeDifficulty(level string) string {
	if level == "medium" {
		return string(model.LevelNormal)
	}
	return level
}
