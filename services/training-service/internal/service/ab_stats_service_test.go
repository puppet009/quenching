// 人机对照数据统计（T3-26）单元测试。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// ——— 内存版仓储（测试用）———

type fakeABStatsPairStore struct {
	pairs []model.ABPair
}

func (f *fakeABStatsPairStore) ListPairs(_ context.Context, _ model.ABFilter) ([]model.ABPair, error) {
	return f.pairs, nil
}

func newABStatsTestService(pairs []model.ABPair) *ABStatsService {
	return NewABStatsService(&fakeABStatsPairStore{pairs: pairs})
}

// ——— 辅助函数 ———

func makePair(userID int64, aiDur int, aiResolved bool, aiDanger int, aiVerify int,
	humanDur int, humanResolved bool, humanDanger int, humanVerify int) model.ABPair {
	return model.ABPair{
		UserID:    userID,
		ProjectID: 1,
		Group:     "A",
		AI: model.AISide{
			SessionID:    userID*10 + 1,
			DurationSec:  aiDur,
			Resolved:     aiResolved,
			DangerousOps: aiDanger,
			VerifyCount:  aiVerify,
		},
		Human: model.HumanSide{
			SessionID:    userID*10 + 2,
			DurationSec:  humanDur,
			Resolved:     humanResolved,
			DangerousOps: humanDanger,
			VerifyCount:  humanVerify,
		},
		CompletedAt: time.Now(),
	}
}

// ========== 汇总统计测试 ==========

func TestSummary_SpeedDiff_AIisFaster(t *testing.T) {
	// AI 平均 900s，人工平均 1200s → 速度差 = (900-1200)/1200 = -25%
	pairs := []model.ABPair{
		makePair(1, 900, true, 1, 3, 1200, true, 2, 1),
		makePair(2, 800, true, 0, 4, 1100, true, 3, 0),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	// AI 平均 = (900+800)/2 = 850
	// 人工平均 = (1200+1100)/2 = 1150
	// 速度差 = (850-1150)/1150 ≈ -26.09%
	if summary.SpeedDiffPct >= -25 || summary.SpeedDiffPct <= -27 {
		t.Errorf("速度差应约 -26%%, got %.2f", summary.SpeedDiffPct)
	}

	if summary.AvgAIDurationSec != 850 {
		t.Errorf("AI 平均耗时应为 850, got %.2f", summary.AvgAIDurationSec)
	}
	if summary.AvgHumanDurationSec != 1150 {
		t.Errorf("人工平均耗时应为 1150, got %.2f", summary.AvgHumanDurationSec)
	}
}

func TestSummary_AccuracyDiff_AIisBetter(t *testing.T) {
	// 3 对：AI 都修复，人工 2/3 修复 → 正确率差 = 100% - 66.67% = 33.33%
	pairs := []model.ABPair{
		makePair(1, 1000, true, 0, 0, 1000, true, 0, 0),
		makePair(2, 1000, true, 0, 0, 1000, true, 0, 0),
		makePair(3, 1000, true, 0, 0, 1000, false, 0, 0),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if summary.AIAccuracyPct != 100.0 {
		t.Errorf("AI 正确率应为 100%%, got %.2f", summary.AIAccuracyPct)
	}
	// 人工正确率 = 2/3 ≈ 66.67%
	if summary.HumanAccuracyPct < 66 || summary.HumanAccuracyPct > 67 {
		t.Errorf("人工正确率应约 66.67%%, got %.2f", summary.HumanAccuracyPct)
	}
	// 正确率差 ≈ 33.33%
	if summary.AccuracyDiffPct < 33 || summary.AccuracyDiffPct > 34 {
		t.Errorf("正确率差应约 33.33%%, got %.2f", summary.AccuracyDiffPct)
	}
}

func TestSummary_DangerousOpDiff_AIisSafer(t *testing.T) {
	// AI 人均 1 次危险操作，人工人均 3 次 → 差 = (1-3)/3 = -66.67%
	pairs := []model.ABPair{
		makePair(1, 1000, true, 1, 0, 1000, true, 3, 0),
		makePair(2, 1000, true, 1, 0, 1000, true, 3, 0),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if summary.AvgAIDangerousOps != 1.0 {
		t.Errorf("AI 人均危险操作应为 1.0, got %.2f", summary.AvgAIDangerousOps)
	}
	if summary.AvgHumanDangerousOps != 3.0 {
		t.Errorf("人工人均危险操作应为 3.0, got %.2f", summary.AvgHumanDangerousOps)
	}
	// 危险操作率差 = (1-3)/3 = -66.67%
	if summary.DangerousOpDiffPct > -66 || summary.DangerousOpDiffPct < -67 {
		t.Errorf("危险操作率差应约 -66.67%%, got %.2f", summary.DangerousOpDiffPct)
	}
}

func TestSummary_VerifyDiff_AIhasMore(t *testing.T) {
	// AI 人均 4 次验证，人工人均 2 次 → 差 = (4-2)/2 = 100%
	pairs := []model.ABPair{
		makePair(1, 1000, true, 0, 4, 1000, true, 0, 2),
		makePair(2, 1000, true, 0, 4, 1000, true, 0, 2),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if summary.AvgAIVerifyCount != 4.0 {
		t.Errorf("AI 人均验证应为 4.0, got %.2f", summary.AvgAIVerifyCount)
	}
	if summary.AvgHumanVerifyCount != 2.0 {
		t.Errorf("人工人均验证应为 2.0, got %.2f", summary.AvgHumanVerifyCount)
	}
	// 验证行为差 = (4-2)/2 = 100%
	if summary.VerifyDiffPct != 100.0 {
		t.Errorf("验证行为差应为 100%%, got %.2f", summary.VerifyDiffPct)
	}
}

func TestSummary_Empty(t *testing.T) {
	svc := newABStatsTestService(nil)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if summary.PairCount != 0 {
		t.Errorf("空数据对照组数应为 0, got %d", summary.PairCount)
	}
	if summary.SpeedDiffPct != 0 {
		t.Errorf("空数据速度差应为 0, got %.2f", summary.SpeedDiffPct)
	}
	if summary.AccuracyDiffPct != 0 {
		t.Errorf("空数据正确率差应为 0, got %.2f", summary.AccuracyDiffPct)
	}
	if summary.SampleInfo.ConfidenceLevel != "低" {
		t.Errorf("空数据置信度应为'低', got %s", summary.SampleInfo.ConfidenceLevel)
	}
}

func TestSummary_PairCount(t *testing.T) {
	pairs := []model.ABPair{
		makePair(1, 1000, true, 0, 0, 1200, true, 0, 0),
		makePair(2, 800, true, 0, 0, 1000, true, 0, 0),
		makePair(3, 900, false, 0, 0, 1100, true, 0, 0),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if summary.PairCount != 3 {
		t.Errorf("对照组数应为 3, got %d", summary.PairCount)
	}
}

// ========== 样本量测试 ==========

func TestSampleInfo_LowConfidence(t *testing.T) {
	info := calcSampleInfo(5, 5)
	if info.EnoughSample {
		t.Error("5 组不应判定为样本充足")
	}
	if info.ConfidenceLevel != "低" {
		t.Errorf("5 组置信度应为'低', got %s", info.ConfidenceLevel)
	}
	if info.PairCount != 5 {
		t.Errorf("PairCount 应为 5, got %d", info.PairCount)
	}
}

func TestSampleInfo_MediumConfidence(t *testing.T) {
	info := calcSampleInfo(15, 15)
	if info.EnoughSample {
		t.Error("15 组不应判定为样本充足")
	}
	if info.ConfidenceLevel != "中" {
		t.Errorf("15 组置信度应为'中', got %s", info.ConfidenceLevel)
	}
}

func TestSampleInfo_HighConfidence(t *testing.T) {
	info := calcSampleInfo(35, 35)
	if !info.EnoughSample {
		t.Error("35 组应判定为样本充足")
	}
	if info.ConfidenceLevel != "高" {
		t.Errorf("35 组置信度应为'高', got %s", info.ConfidenceLevel)
	}
}

func TestSampleInfo_Zero(t *testing.T) {
	info := calcSampleInfo(0, 0)
	if info.EnoughSample {
		t.Error("0 组不应判定为样本充足")
	}
	if info.ConfidenceLevel != "低" {
		t.Errorf("0 组置信度应为'低', got %s", info.ConfidenceLevel)
	}
	if info.Tip == "" {
		t.Error("提示文案不应为空")
	}
}

// ========== 按项目统计测试 ==========

func TestListByProject_Basic(t *testing.T) {
	pairs := []model.ABPair{
		// 项目1：2 对
		makePair(1, 900, true, 0, 0, 1200, true, 0, 0),
		makePair(2, 800, true, 0, 0, 1000, true, 0, 0),
		// 项目2：1 对
		{
			UserID: 3, ProjectID: 2, Group: "B",
			AI:    model.AISide{DurationSec: 600, Resolved: true},
			Human: model.HumanSide{DurationSec: 800, Resolved: false},
		},
	}

	svc := newABStatsTestService(pairs)
	result, err := svc.ListByProject(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("应有 2 个项目, got %d", len(result))
	}

	// 按对照组数降序，项目1(2) > 项目2(1)
	if result[0].ProjectID != 1 {
		t.Errorf("第一名应是项目1, got project %d", result[0].ProjectID)
	}
	if result[0].PairCount != 2 {
		t.Errorf("项目1对照数应为 2, got %d", result[0].PairCount)
	}

	// 项目2：AI 100% 修复，人工 0% → 正确率差 = 100%
	if result[1].ProjectID != 2 {
		t.Errorf("第二名应是项目2, got project %d", result[1].ProjectID)
	}
	if result[1].AccuracyDiffPct != 100.0 {
		t.Errorf("项目2正确率差应为 100%%, got %.2f", result[1].AccuracyDiffPct)
	}
}

func TestListByProject_Empty(t *testing.T) {
	svc := newABStatsTestService(nil)
	result, err := svc.ListByProject(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("空数据应返回空数组, got %d 条", len(result))
	}
}

// ========== 边界情况测试 ==========

func TestSummary_HumanZeroDangerous(t *testing.T) {
	// 人工危险操作为 0，AI 有 2 次 → 用绝对差
	pairs := []model.ABPair{
		makePair(1, 1000, true, 2, 0, 1000, true, 0, 0),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	// 人工为 0 时，应该用绝对差表示（2 次 × 100）
	if summary.DangerousOpDiffPct <= 0 {
		t.Errorf("人工为0时AI有危险操作，差应为正值, got %.2f", summary.DangerousOpDiffPct)
	}
}

func TestSummary_HumanZeroVerify(t *testing.T) {
	// 人工验证为 0，AI 有 3 次 → 用绝对差
	pairs := []model.ABPair{
		makePair(1, 1000, true, 0, 3, 1000, true, 0, 0),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if summary.VerifyDiffPct <= 0 {
		t.Errorf("人工为0时AI有验证，差应为正值, got %.2f", summary.VerifyDiffPct)
	}
}

func TestSummary_AllZeros(t *testing.T) {
	// 两边都是 0 的情况
	pairs := []model.ABPair{
		makePair(1, 1000, true, 0, 0, 1000, true, 0, 0),
	}

	svc := newABStatsTestService(pairs)
	summary, err := svc.GetSummary(context.Background(), model.ABFilter{})
	if err != nil {
		t.Fatal(err)
	}

	// 危险操作都为 0 时，差应为 0
	if summary.DangerousOpDiffPct != 0 {
		t.Errorf("两边都为0时危险操作差应为 0, got %.2f", summary.DangerousOpDiffPct)
	}
	// 验证都为 0 时，差应为 0
	if summary.VerifyDiffPct != 0 {
		t.Errorf("两边都为0时验证差应为 0, got %.2f", summary.VerifyDiffPct)
	}
}
