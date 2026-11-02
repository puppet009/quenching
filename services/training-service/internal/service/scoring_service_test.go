// 评分体系单元测试（T2-6）。
package service

import (
	"context"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// ========== 等级判定测试 ==========

func TestGetGrade_Excellent(t *testing.T) {
	g := getGrade(95.0)
	if g.Grade != model.GradeExcellent {
		t.Errorf("95 分应为优秀, got %s", g.Grade)
	}
	if g.Name != "优秀" {
		t.Errorf("等级名称应为优秀, got %s", g.Name)
	}
}

func TestGetGrade_Good(t *testing.T) {
	g := getGrade(85.0)
	if g.Grade != model.GradeGood {
		t.Errorf("85 分应为良好, got %s", g.Grade)
	}
}

func TestGetGrade_Pass(t *testing.T) {
	g := getGrade(70.0)
	if g.Grade != model.GradePass {
		t.Errorf("70 分应为及格, got %s", g.Grade)
	}
}

func TestGetGrade_Fail(t *testing.T) {
	g := getGrade(50.0)
	if g.Grade != model.GradeFail {
		t.Errorf("50 分应为不及格, got %s", g.Grade)
	}
}

func TestGetGrade_Boundary(t *testing.T) {
	cases := []struct {
		score float64
		grade model.Grade
	}{
		{100, model.GradeExcellent},
		{90, model.GradeExcellent},
		{89.99, model.GradeGood},
		{80, model.GradeGood},
		{79.99, model.GradePass},
		{60, model.GradePass},
		{59.99, model.GradeFail},
		{0, model.GradeFail},
	}

	for _, c := range cases {
		g := getGrade(c.score)
		if g.Grade != c.grade {
			t.Errorf("%.2f 分应为 %s, got %s", c.score, c.grade, g.Grade)
		}
	}
}

// ========== 排障速度评分测试 ==========

func TestCalcSpeedScore_WithinExpected(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.3}

	input := model.ScoreInput{
		FaultStartTime:  1000000, // 1000 秒
		RecoveryTime:    1500000, // 1500 秒 = 500 秒用时
		ExpectedSeconds: 600,     // 预期 600 秒
	}

	svc.calcSpeedScore(ds, input)

	if ds.Score != 100 {
		t.Errorf("在预期时间内应得 100 分, got %.2f", ds.Score)
	}
}

func TestCalcSpeedScore_OverExpected(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.3}

	input := model.ScoreInput{
		FaultStartTime:  1000000, // 1000 秒
		RecoveryTime:    3000000, // 3000 秒 = 2000 秒用时
		ExpectedSeconds: 1000,    // 预期 1000 秒
	}

	svc.calcSpeedScore(ds, input)

	// 实际 2000 秒，预期 1000 秒，比例 0.5，得分 50
	if ds.Score != 50 {
		t.Errorf("超时一倍应得 50 分, got %.2f", ds.Score)
	}

	if len(ds.Deductions) == 0 {
		t.Error("应有扣分明细")
	}
}

func TestCalcSpeedScore_NoRecovery(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.3}

	input := model.ScoreInput{
		FaultStartTime:  1000000,
		RecoveryTime:    0, // 未恢复
		ExpectedSeconds: 600,
	}

	svc.calcSpeedScore(ds, input)

	if ds.Score != 0 {
		t.Errorf("未恢复应得 0 分, got %.2f", ds.Score)
	}
}

func TestCalcSpeedScore_InstantRecovery(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.3}

	input := model.ScoreInput{
		FaultStartTime:  1000000,
		RecoveryTime:    1001000, // 1 秒
		ExpectedSeconds: 600,
	}

	svc.calcSpeedScore(ds, input)

	if ds.Score != 100 {
		t.Errorf("秒级恢复应得 100 分, got %.2f", ds.Score)
	}
}

// ========== 恢复质量评分测试 ==========

func TestCalcRecoveryScore_Full(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.25}

	input := model.ScoreInput{RecoveryScore: 100}
	svc.calcRecoveryScore(ds, input)

	if ds.Score != 100 {
		t.Errorf("满分恢复应得 100 分, got %.2f", ds.Score)
	}
}

func TestCalcRecoveryScore_Partial(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.25}

	input := model.ScoreInput{RecoveryScore: 75.5}
	svc.calcRecoveryScore(ds, input)

	if ds.Score != 75.5 {
		t.Errorf("恢复得分 75.5 应原样传入, got %.2f", ds.Score)
	}
}

func TestCalcRecoveryScore_Clamp(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.25}

	// 超过 100 应截断
	input := model.ScoreInput{RecoveryScore: 120}
	svc.calcRecoveryScore(ds, input)

	if ds.Score != 100 {
		t.Errorf("超过 100 应截断为 100, got %.2f", ds.Score)
	}

	// 低于 0 应截断
	input2 := model.ScoreInput{RecoveryScore: -10}
	ds2 := &model.DimensionScore{Weight: 0.25}
	svc.calcRecoveryScore(ds2, input2)

	if ds2.Score != 0 {
		t.Errorf("低于 0 应截断为 0, got %.2f", ds2.Score)
	}
}

// ========== 方法合理性评分测试 ==========

func TestCalcMethodScore(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.2}

	input := model.ScoreInput{MethodScore: 80}
	svc.calcMethodScore(ds, input)

	if ds.Score != 80 {
		t.Errorf("方法得分 80 应原样传入, got %.2f", ds.Score)
	}
}

// ========== 知识掌握评分测试 ==========

func TestCalcKnowledgeScore(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.15}

	input := model.ScoreInput{KnowledgeScore: 90}
	svc.calcKnowledgeScore(ds, input)

	if ds.Score != 90 {
		t.Errorf("知识得分 90 应原样传入, got %.2f", ds.Score)
	}
}

// ========== 团队协作评分测试 ==========

func TestCalcCollabScore_Solo(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.1}

	// 单人模式，协作分为 0，应默认满分
	input := model.ScoreInput{CollabScore: 0}
	svc.calcCollabScore(ds, input)

	if ds.Score != 100 {
		t.Errorf("单人模式协作应默认满分, got %.2f", ds.Score)
	}
}

func TestCalcCollabScore_Team(t *testing.T) {
	svc := NewScoringService()
	ds := &model.DimensionScore{Weight: 0.1}

	input := model.ScoreInput{CollabScore: 85}
	svc.calcCollabScore(ds, input)

	if ds.Score != 85 {
		t.Errorf("团队协作得分 85 应原样传入, got %.2f", ds.Score)
	}
}

// ========== 综合评分测试 ==========

func TestCalculate_PerfectScore(t *testing.T) {
	svc := NewScoringService()

	input := model.ScoreInput{
		SessionID:       "sess-perfect",
		UserID:          1,
		FaultStartTime:  1000000,
		RecoveryTime:    1500000, // 500 秒
		RecoveryScore:   100,
		MethodScore:     100,
		KnowledgeScore:  100,
		CollabScore:     100,
		ExpectedSeconds: 600,
	}

	result, err := svc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalScore != 100.0 {
		t.Errorf("全满分应得 100 分, got %.2f", result.TotalScore)
	}
	if result.Grade.Grade != model.GradeExcellent {
		t.Errorf("满分应为优秀, got %s", result.Grade.Grade)
	}
	if len(result.Dimensions) != 5 {
		t.Errorf("应有 5 个维度, got %d", len(result.Dimensions))
	}

	// 验证加权计算
	// speed: 100 * 0.3 = 30
	// recovery: 100 * 0.25 = 25
	// method: 100 * 0.2 = 20
	// knowledge: 100 * 0.15 = 15
	// collaboration: 100 * 0.1 = 10
	// 总计: 100
	totalWeighted := 0.0
	for _, d := range result.Dimensions {
		totalWeighted += d.Weighted
	}
	if totalWeighted != 100.0 {
		t.Errorf("加权总分应为 100, got %.2f", totalWeighted)
	}
}

func TestCalculate_MixedScore(t *testing.T) {
	svc := NewScoringService()

	input := model.ScoreInput{
		SessionID:       "sess-mixed",
		UserID:          1,
		FaultStartTime:  1000000,
		RecoveryTime:    3000000, // 2000 秒
		RecoveryScore:   80,
		MethodScore:     70,
		KnowledgeScore:  85,
		CollabScore:     90,
		ExpectedSeconds: 1000,
	}

	result, err := svc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	// speed: 50 * 0.3 = 15
	// recovery: 80 * 0.25 = 20
	// method: 70 * 0.2 = 14
	// knowledge: 85 * 0.15 = 12.75
	// collaboration: 90 * 0.1 = 9
	// 总计: 70.75
	expected := 70.75
	if result.TotalScore != expected {
		t.Errorf("总分应为 %.2f, got %.2f", expected, result.TotalScore)
	}

	if result.Grade.Grade != model.GradePass {
		t.Errorf("70.75 分应为及格, got %s", result.Grade.Grade)
	}
}

func TestCalculate_ZeroScore(t *testing.T) {
	svc := NewScoringService()

	input := model.ScoreInput{
		SessionID:      "sess-zero",
		UserID:         1,
		FaultStartTime: 1000000,
		RecoveryTime:   0, // 未恢复
		RecoveryScore:  0,
		MethodScore:    0,
		KnowledgeScore: 0,
		CollabScore:    0,
	}

	result, err := svc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	// speed: 0 * 0.3 = 0
	// recovery: 0 * 0.25 = 0
	// method: 0 * 0.2 = 0
	// knowledge: 0 * 0.15 = 0
	// collaboration: 100 * 0.1 = 10 (单人模式默认满分)
	// 总计: 10
	if result.TotalScore != 10.0 {
		t.Errorf("总分应为 10 分（只有协作满分）, got %.2f", result.TotalScore)
	}

	if result.Grade.Grade != model.GradeFail {
		t.Errorf("10 分应为不及格, got %s", result.Grade.Grade)
	}
}

// ========== 结果保存与查询测试 ==========

func TestGetResult(t *testing.T) {
	svc := NewScoringService()

	input := model.ScoreInput{
		SessionID:      "sess-test",
		UserID:         123,
		RecoveryScore:  90,
		MethodScore:    80,
		KnowledgeScore: 85,
		CollabScore:    95,
	}

	_, err := svc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	result, exists := svc.GetResult("sess-test")
	if !exists {
		t.Fatal("应能找到评分结果")
	}

	if result.SessionID != "sess-test" {
		t.Errorf("会话ID不匹配")
	}
	if result.UserID != 123 {
		t.Errorf("用户ID不匹配")
	}
}

func TestGetResult_NotFound(t *testing.T) {
	svc := NewScoringService()

	_, exists := svc.GetResult("nonexistent")
	if exists {
		t.Error("不存在的会话不应有结果")
	}
}

func TestListResults(t *testing.T) {
	svc := NewScoringService()

	// 添加多个结果
	for i := 0; i < 5; i++ {
		input := model.ScoreInput{
			SessionID:      "sess-" + string(rune('a'+i)),
			UserID:         int64(i % 2), // 用户 0 和 1
			RecoveryScore:  float64(80 + i),
			MethodScore:    80,
			KnowledgeScore: 80,
			CollabScore:    80,
		}
		svc.Calculate(context.Background(), input)
	}

	// 列出用户 0 的结果
	results := svc.ListResults(0, 0)
	if len(results) != 3 {
		t.Errorf("用户 0 应有 3 条记录, got %d", len(results))
	}

	// 限制条数
	results2 := svc.ListResults(0, 2)
	if len(results2) != 2 {
		t.Errorf("限制 2 条应返回 2 条, got %d", len(results2))
	}
}

// ========== 维度配置测试 ==========

func TestGetDimensions(t *testing.T) {
	svc := NewScoringService()

	dims := svc.GetDimensions()
	if len(dims) != 5 {
		t.Errorf("应有 5 个评分维度, got %d", len(dims))
	}

	// 验证权重总和为 1
	totalWeight := 0.0
	for _, d := range dims {
		totalWeight += d.Weight
	}
	if totalWeight != 1.0 {
		t.Errorf("权重总和应为 1.0, got %.2f", totalWeight)
	}
}

// ========== 反馈生成测试 ==========

func TestGenerateOverallFeedback(t *testing.T) {
	svc := NewScoringService()

	grades := []model.Grade{model.GradeExcellent, model.GradeGood, model.GradePass, model.GradeFail}
	for _, g := range grades {
		result := &model.ScoreResult{TotalScore: gMinScore(g), Grade: getGrade(gMinScore(g))}
		feedback := svc.generateOverallFeedback(result)
		if feedback == "" {
			t.Errorf("%s 等级的反馈不应为空", g)
		}
	}
}

// 辅助函数：获取某等级的最低分
func gMinScore(g model.Grade) float64 {
	for _, gt := range gradeTable {
		if gt.Grade == g {
			return gt.MinScore
		}
	}
	return 0
}

// ========== 工具函数测试 ==========

func TestRound2(t *testing.T) {
	cases := []struct {
		input    float64
		expected float64
	}{
		{80.123, 80.12},
		{80.126, 80.13},
		{100.0, 100.0},
		{0.0, 0.0},
		{75.555, 75.56},
	}

	for _, c := range cases {
		result := round2(c.input)
		if result != c.expected {
			t.Errorf("round2(%.3f) = %.2f, 期望 %.2f", c.input, result, c.expected)
		}
	}
}
