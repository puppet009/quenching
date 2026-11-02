// 人机对照数据统计服务。
package service

import (
	"context"
	"fmt"

	"github.com/quenching/training-service/internal/model"
)

// ABStatsPairStore 对照数据仓储端口。
// PG 实现在 repository.ABStatsRepo，内存实现在测试中。
type ABStatsPairStore interface {
	// ListPairs 列出满足筛选条件的所有对照配对。
	ListPairs(ctx context.Context, filter model.ABFilter) ([]model.ABPair, error)
}

// ABStatsService 对照统计服务。
type ABStatsService struct {
	store ABStatsPairStore
}

// NewABStatsService 创建对照统计服务。
func NewABStatsService(store ABStatsPairStore) *ABStatsService {
	return &ABStatsService{store: store}
}

// GetSummary 获取对照统计汇总。
//
// 计算四项核心差异：
//   - 速度差 = (AI平均耗时 - 人工平均耗时) / 人工平均耗时 × 100%
//   - 正确率差 = AI正确率 - 人工正确率
//   - 危险操作率差 = (AI人均危险操作 - 人工人均危险操作) / 人工人均危险操作 × 100%
//     （人工为 0 时用绝对差）
//   - 验证行为差 = (AI人均验证 - 人工人均验证) / 人工人均验证 × 100%
//     （人工为 0 时用绝对差）
func (s *ABStatsService) GetSummary(ctx context.Context, filter model.ABFilter) (*model.ABSummary, error) {
	pairs, err := s.store.ListPairs(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("查询对照数据失败: %w", err)
	}

	summary := &model.ABSummary{PairCount: len(pairs)}

	if len(pairs) == 0 {
		summary.SampleInfo = calcSampleInfo(0, 0)
		return summary, nil
	}

	// 累加各项指标
	var (
		totalAIDur, totalHumanDur             int
		aiResolved, humanResolved             int
		totalAIDangerous, totalHumanDangerous int
		totalAIVerify, totalHumanVerify       int
	)

	userSet := make(map[int64]bool)

	for _, p := range pairs {
		userSet[p.UserID] = true

		totalAIDur += p.AI.DurationSec
		totalHumanDur += p.Human.DurationSec

		if p.AI.Resolved {
			aiResolved++
		}
		if p.Human.Resolved {
			humanResolved++
		}

		totalAIDangerous += p.AI.DangerousOps
		totalHumanDangerous += p.Human.DangerousOps

		totalAIVerify += p.AI.VerifyCount
		totalHumanVerify += p.Human.VerifyCount
	}

	n := float64(len(pairs))
	userCount := len(userSet)

	// 平均耗时
	summary.AvgAIDurationSec = round2(float64(totalAIDur) / n)
	summary.AvgHumanDurationSec = round2(float64(totalHumanDur) / n)

	// 速度差
	if totalHumanDur > 0 {
		summary.SpeedDiffPct = round2((float64(totalAIDur) - float64(totalHumanDur)) / float64(totalHumanDur) * 100)
	}

	// 正确率
	summary.AIAccuracyPct = round2(float64(aiResolved) / n * 100)
	summary.HumanAccuracyPct = round2(float64(humanResolved) / n * 100)
	summary.AccuracyDiffPct = round2(summary.AIAccuracyPct - summary.HumanAccuracyPct)

	// 危险操作（人均）
	summary.AvgAIDangerousOps = round2(float64(totalAIDangerous) / n)
	summary.AvgHumanDangerousOps = round2(float64(totalHumanDangerous) / n)

	if totalHumanDangerous > 0 {
		summary.DangerousOpDiffPct = round2(
			(summary.AvgAIDangerousOps - summary.AvgHumanDangerousOps) / summary.AvgHumanDangerousOps * 100)
	} else if totalAIDangerous > 0 {
		// 人工为 0 但 AI > 0，用绝对差（正数表示 AI 更多）
		summary.DangerousOpDiffPct = round2(summary.AvgAIDangerousOps * 100)
	}

	// 验证行为（人均）
	summary.AvgAIVerifyCount = round2(float64(totalAIVerify) / n)
	summary.AvgHumanVerifyCount = round2(float64(totalHumanVerify) / n)

	if totalHumanVerify > 0 {
		summary.VerifyDiffPct = round2(
			(summary.AvgAIVerifyCount - summary.AvgHumanVerifyCount) / summary.AvgHumanVerifyCount * 100)
	} else if totalAIVerify > 0 {
		// 人工为 0 但 AI > 0，用绝对差
		summary.VerifyDiffPct = round2(summary.AvgAIVerifyCount * 100)
	}

	// 样本量
	summary.SampleInfo = calcSampleInfo(len(pairs), userCount)

	return summary, nil
}

// ListByProject 按项目维度汇总对照统计。
func (s *ABStatsService) ListByProject(ctx context.Context, filter model.ABFilter) ([]model.ProjectABStat, error) {
	pairs, err := s.store.ListPairs(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("查询对照数据失败: %w", err)
	}

	// 按项目分组
	type projectAcc struct {
		count         int
		totalAIDur    int
		totalHumanDur int
		aiResolved    int
		humanResolved int
	}

	projects := make(map[int64]*projectAcc)

	for _, p := range pairs {
		acc, ok := projects[p.ProjectID]
		if !ok {
			acc = &projectAcc{}
			projects[p.ProjectID] = acc
		}
		acc.count++
		acc.totalAIDur += p.AI.DurationSec
		acc.totalHumanDur += p.Human.DurationSec
		if p.AI.Resolved {
			acc.aiResolved++
		}
		if p.Human.Resolved {
			acc.humanResolved++
		}
	}

	result := make([]model.ProjectABStat, 0, len(projects))

	for pid, acc := range projects {
		n := float64(acc.count)
		speedDiff := 0.0
		if acc.totalHumanDur > 0 {
			speedDiff = round2(float64(acc.totalAIDur-acc.totalHumanDur) / float64(acc.totalHumanDur) * 100)
		}
		accDiff := round2(float64(acc.aiResolved)/n*100 - float64(acc.humanResolved)/n*100)

		result = append(result, model.ProjectABStat{
			ProjectID:       pid,
			PairCount:       acc.count,
			SpeedDiffPct:    speedDiff,
			AccuracyDiffPct: accDiff,
		})
	}

	// 按对照组数降序
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].PairCount > result[i].PairCount {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// ——— 样本量计算 ———

const (
	// minPairsForConfidence 高置信度所需最少对照组数。
	minPairsForConfidence = 30
	// minPairsForMedium 中置信度所需最少对照组数。
	minPairsForMedium = 10
)

// calcSampleInfo 计算样本量信息。
func calcSampleInfo(pairCount int, userCount int) model.SampleInfo {
	info := model.SampleInfo{
		PairCount: pairCount,
		UserCount: userCount,
	}

	switch {
	case pairCount >= minPairsForConfidence:
		info.EnoughSample = true
		info.Tip = "样本量充足，对照结果具有统计意义"
		info.ConfidenceLevel = "高"
	case pairCount >= minPairsForMedium:
		info.EnoughSample = false
		info.Tip = "样本量一般（10-29组），结果有参考价值但需谨慎解读"
		info.ConfidenceLevel = "中"
	default:
		info.EnoughSample = false
		info.Tip = "样本量不足（<10组），结果仅供参考，不建议作为决策依据"
		info.ConfidenceLevel = "低"
	}

	return info
}
