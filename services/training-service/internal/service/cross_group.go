// Package service 中人机对照交叉分组（T3-25）的纯领域规则。
//
// 交叉设计（《技术架构文档》§5.2）：新用户首训前随机分 A/B 组，
// A 组顺序 人工(none) -> AI(assistant)，B 组顺序 AI -> 人工，
// 消除"第二次更熟练"的顺序效应；两次训练使用不同故障但同类同难度，
// 消除"第二次故障更简单"的难度偏差。本文件只放无副作用的纯函数，
// 持久化、随机源、跨服务访问见 cross_group_service.go 与各端口。
package service

import (
	"fmt"

	"github.com/quenching/training-service/internal/model"
)

// CrossGroupAttempt 交叉设计覆盖的训练序号：只管前两次。
const (
	AttemptFirst  = 1
	AttemptSecond = 2
)

// PlanAIMode 按 (分组, 第几次训练) 决定本次 ai_mode（A0/A1）。
//
//	A 组（人工->AI）：第 1 次 none，第 2 次 assistant。
//	B 组（AI->人工）：第 1 次 assistant，第 2 次 none。
//
// 交叉设计只覆盖前两次训练；attempt 超出 [1,2] 或分组非法一律报错，
// 由编排层在"第 3 次及以后"改为普通训练流程，不套用对照模式。
func PlanAIMode(group model.ControlGroup, attempt int) (model.AIMode, error) {
	if attempt != AttemptFirst && attempt != AttemptSecond {
		return "", fmt.Errorf("交叉分组仅覆盖前两次训练，收到第 %d 次", attempt)
	}
	switch group {
	case model.GroupA:
		if attempt == AttemptFirst {
			return model.AIModeNone, nil
		}
		return model.AIModeAssist, nil
	case model.GroupB:
		if attempt == AttemptFirst {
			return model.AIModeAssist, nil
		}
		return model.AIModeNone, nil
	default:
		return "", fmt.Errorf("非法对照分组: %q", group)
	}
}

// ModesComplement 两次训练的 ai_mode 是否恰好互补（一次 none、一次 assistant）。
// 配对绑定时校验，防止把两次同模式会话错配成对照对。
func ModesComplement(m1, m2 model.AIMode) bool {
	return (m1 == model.AIModeNone && m2 == model.AIModeAssist) ||
		(m1 == model.AIModeAssist && m2 == model.AIModeNone)
}

// PairingDefect 配对选故障所需的最小故障信息（来自 fault-engine.defects +
// session_defects 的只读投影）。Category/Difficulty 是"同类同难度"的校准维度。
type PairingDefect struct {
	ID         int64
	Code       string
	Category   string
	Difficulty string
}

// ChoosePairedDefect 为第二次训练挑选"不同故障、但同类同难度"的校准故障。
//
// 入参 first 为第一次训练实际挂载的故障（候选为空时以它所属类目/难度为校准锚点）；
// used 为第一次已用故障 id 集合（必须排除，避免两次同一个故障）；
// candidates 为内容库中可选故障。筛选规则：
//   - category 与首场完全相同（同类，如都是 database）；
//   - difficulty 与首场完全相同（同难度，难度值取 defects.difficulty 原值）；
//   - id 不在 used 中（不同故障）；
//   - 在满足条件者中取 id 最小者，保证选择确定、可重放。
//
// 找不到合格候选时返回错误，由编排层提示内容库缺少同类故障，
// 而不是悄悄放宽难度/类目（放宽会污染对照数据）。
func ChoosePairedDefect(first PairingDefect, used map[int64]bool, candidates []PairingDefect) (PairingDefect, error) {
	if first.ID <= 0 {
		return PairingDefect{}, fmt.Errorf("缺少首场故障，无法确定校准类目/难度")
	}
	best := PairingDefect{}
	found := false
	for _, c := range candidates {
		if c.ID <= 0 || used[c.ID] {
			continue
		}
		if c.Category != first.Category || c.Difficulty != first.Difficulty {
			continue
		}
		if !found || c.ID < best.ID {
			best = c
			found = true
		}
	}
	if !found {
		return PairingDefect{}, fmt.Errorf(
			"没有与首场故障同类(%s)同难度(%s)的其他故障可供第二次训练", first.Category, first.Difficulty)
	}
	return best, nil
}
