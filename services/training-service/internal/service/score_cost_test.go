// 操作代价计分单元测试（T2-17）。
package service

import (
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// ========== 基础代价测试 ==========

// TestCostScore_NoActions 无操作 → 满分。
func TestCostScore_NoActions(t *testing.T) {
	score := CostScore(nil)
	if score != 100 {
		t.Errorf("无操作应得 100 分, got %.2f", score)
	}
}

// TestCostScore_InfoOnly 只有信息收集操作 → 高分。
func TestCostScore_InfoOnly(t *testing.T) {
	actions := []model.OperationAction{
		{Type: model.OpViewMetric, TimestampMs: 10000, IsEffective: true},
		{Type: model.OpViewLog, TimestampMs: 20000, IsEffective: true},
		{Type: model.OpViewAlert, TimestampMs: 30000, IsEffective: true},
		{Type: model.OpViewConfig, TimestampMs: 40000, IsEffective: true},
	}

	// 基础代价：1 + 1 + 1 + 1 = 4
	// 无重复惩罚（每种 1 次）
	// 无盲目操作惩罚
	// 总分 = 100 - 4 = 96
	score := CostScore(actions)
	expected := 96.0
	if score != expected {
		t.Errorf("仅信息收集应得 %.2f 分, got %.2f", expected, score)
	}
}

// TestCostScore_WithChange 有变更操作但观察充分 → 中等分。
func TestCostScore_WithChange(t *testing.T) {
	actions := []model.OperationAction{
		{Type: model.OpViewMetric, TimestampMs: 10000},
		{Type: model.OpViewLog, TimestampMs: 20000},
		{Type: model.OpViewAlert, TimestampMs: 30000},
		{Type: model.OpViewConfig, TimestampMs: 40000},
		{Type: model.OpExecCommand, TimestampMs: 50000},
		{Type: model.OpModifyConfig, TimestampMs: 60000},
		{Type: model.OpFixComplete, TimestampMs: 70000},
	}

	// 基础代价：1+1+1+1 + 3 + 5 + 0 = 12
	// 无重复惩罚
	// 无盲目操作（观察 4 次 >= 3）
	// 总分 = 100 - 12 = 88
	score := CostScore(actions)
	expected := 88.0
	if score != expected {
		t.Errorf("有变更但观察充分应得 %.2f 分, got %.2f", expected, score)
	}
}

// ========== 重复操作惩罚测试 ==========

// TestCostScore_RepeatOps 重复操作有额外惩罚。
func TestCostScore_RepeatOps(t *testing.T) {
	// 5 次相同的查看指标操作
	actions := make([]model.OperationAction, 5)
	for i := 0; i < 5; i++ {
		actions[i] = model.OperationAction{
			Type:        model.OpViewMetric,
			TimestampMs: int64(i * 10000),
		}
	}

	result := CalcCostPenalty(actions)

	// 基础代价：5 × 1 = 5
	if result.BaseCost != 5.0 {
		t.Errorf("基础代价应为 5.0, got %.2f", result.BaseCost)
	}

	// 重复惩罚倍率：1 + 1 + 1.5 + 2 + 2.5 = 8 倍 → 总代价 = 8 × 1 = 8
	// 额外惩罚 = 8 - 5 = 3
	if result.RepeatPenalty != 3.0 {
		t.Errorf("重复惩罚应为 3.0, got %.2f", result.RepeatPenalty)
	}

	// 总惩罚 = 5 + 3 + 0 = 8
	// 得分 = 92
	score := 100 - result.Penalty
	if score != 92.0 {
		t.Errorf("重复操作后得分应为 92.0, got %.2f", score)
	}
}

// TestCostScore_RepeatMultiplierCapped 重复倍率封顶为 3.0。
func TestCostScore_RepeatMultiplierCapped(t *testing.T) {
	// 10 次重启服务（危险操作）
	actions := make([]model.OperationAction, 10)
	for i := 0; i < 10; i++ {
		actions[i] = model.OperationAction{
			Type:        model.OpRestartService,
			TimestampMs: int64(i * 60000),
		}
	}

	result := CalcCostPenalty(actions)

	// 验证封顶：超过 MaxPenalty(80) 应截断
	if result.Penalty > 80.0 {
		t.Errorf("惩罚分不应超过 80（封顶）, got %.2f", result.Penalty)
	}

	score := 100 - result.Penalty
	if score < 20.0 {
		t.Errorf("得分不应低于 20（保底）, got %.2f", score)
	}
}

// ========== 盲目操作惩罚测试 ==========

// TestCostScore_BlindAction 未充分观察就动手 → 有盲目惩罚。
func TestCostScore_BlindAction(t *testing.T) {
	// 只看了 1 次指标就直接扩容（观察 1 次 < 阈值 3）
	actions := []model.OperationAction{
		{Type: model.OpViewMetric, TimestampMs: 10000},
		{Type: model.OpScaleResource, TimestampMs: 20000},
	}

	result := CalcCostPenalty(actions)

	// 基础代价：1 + 5 = 6
	// 盲目操作惩罚：1 次 × 5 = 5
	// 总惩罚 = 6 + 5 = 11
	expectedPenalty := 11.0
	if result.Penalty != expectedPenalty {
		t.Errorf("盲目操作总惩罚应为 %.2f, got %.2f (base=%.2f, blind=%.2f)",
			expectedPenalty, result.Penalty, result.BaseCost, result.BlindPenalty)
	}

	if result.BlindPenalty != 5.0 {
		t.Errorf("盲目操作惩罚应为 5.0, got %.2f", result.BlindPenalty)
	}
}

// TestCostScore_NoBlindIfEnoughObservation 观察充分则无盲目惩罚。
func TestCostScore_NoBlindIfEnoughObservation(t *testing.T) {
	// 先观察 5 次再变更
	actions := []model.OperationAction{
		{Type: model.OpViewMetric, TimestampMs: 10000},
		{Type: model.OpViewLog, TimestampMs: 20000},
		{Type: model.OpViewAlert, TimestampMs: 30000},
		{Type: model.OpViewConfig, TimestampMs: 40000},
		{Type: model.OpExecCommand, TimestampMs: 50000},
		{Type: model.OpModifyConfig, TimestampMs: 60000},
	}

	result := CalcCostPenalty(actions)

	if result.BlindPenalty != 0 {
		t.Errorf("观察充分不应有盲目惩罚, got %.2f", result.BlindPenalty)
	}
}

// ========== 综合场景测试 ==========

// TestCostScore_IdealPath 理想路径（先观察再诊断再修复）→ 高分。
func TestCostScore_IdealPath(t *testing.T) {
	actions := []model.OperationAction{
		// 观察阶段
		{Type: model.OpViewAlert, TimestampMs: 10000},
		{Type: model.OpViewMetric, TimestampMs: 20000},
		{Type: model.OpViewLog, TimestampMs: 30000},
		// 诊断阶段
		{Type: model.OpExecCommand, TimestampMs: 50000},
		// 确认后修复
		{Type: model.OpModifyConfig, TimestampMs: 80000},
		{Type: model.OpFixComplete, TimestampMs: 100000},
	}

	// 基础代价：1+1+1 + 3 + 5 + 0 = 11
	// 无重复惩罚
	// 无盲目操作（观察 3 次 >= 3）
	// 得分 = 89
	score := CostScore(actions)
	expected := 89.0
	if score != expected {
		t.Errorf("理想路径应得 %.2f 分, got %.2f", expected, score)
	}
}

// TestCostScore_BadPath 糟糕路径（盲目操作+大量重复+危险操作）→ 低分。
func TestCostScore_BadPath(t *testing.T) {
	actions := []model.OperationAction{
		// 没怎么看就直接重启
		{Type: model.OpViewMetric, TimestampMs: 5000},
		{Type: model.OpRestartService, TimestampMs: 10000},
		// 没好再重启一次
		{Type: model.OpRestartService, TimestampMs: 30000},
		// 又盲目扩容
		{Type: model.OpScaleResource, TimestampMs: 50000},
		// 重复查日志
		{Type: model.OpViewLog, TimestampMs: 60000},
		{Type: model.OpViewLog, TimestampMs: 70000},
		{Type: model.OpViewLog, TimestampMs: 80000},
	}

	result := CalcCostPenalty(actions)

	// 验证：惩罚分应该较高
	if result.Penalty < 20 {
		t.Errorf("糟糕路径惩罚分应较高, got %.2f", result.Penalty)
	}

	score := 100 - result.Penalty
	if score > 80 {
		t.Errorf("糟糕路径得分应低于 80, got %.2f", score)
	}

	// 验证有盲目操作惩罚（第 1 次重启前只有 1 次观察）
	if result.BlindPenalty <= 0 {
		t.Error("应有盲目操作惩罚")
	}
}

// ========== 代价明细测试 ==========

// TestCostResult_Breakdown 验证各类型操作明细。
func TestCostResult_Breakdown(t *testing.T) {
	actions := []model.OperationAction{
		{Type: model.OpViewMetric, TimestampMs: 10000},
		{Type: model.OpViewMetric, TimestampMs: 20000},
		{Type: model.OpViewLog, TimestampMs: 30000},
		{Type: model.OpExecCommand, TimestampMs: 40000},
	}

	result := CalcCostPenalty(actions)

	// 验证各类型统计
	if result.Breakdown[model.OpViewMetric].Count != 2 {
		t.Errorf("查看指标次数应为 2, got %d", result.Breakdown[model.OpViewMetric].Count)
	}

	if result.Breakdown[model.OpViewLog].Count != 1 {
		t.Errorf("查看日志次数应为 1, got %d", result.Breakdown[model.OpViewLog].Count)
	}

	if result.Breakdown[model.OpExecCommand].Count != 1 {
		t.Errorf("执行命令次数应为 1, got %d", result.Breakdown[model.OpExecCommand].Count)
	}

	// 验证总代价 = 基础 + 额外
	for _, stat := range result.Breakdown {
		if stat.TotalCost != stat.BaseCost+stat.ExtraCost {
			t.Errorf("总代价计算错误: total=%.2f, base=%.2f, extra=%.2f",
				stat.TotalCost, stat.BaseCost, stat.ExtraCost)
		}
	}
}

// ========== 配置自定义测试 ==========

// TestCostScore_CustomConfig 自定义配置生效。
func TestCostScore_CustomConfig(t *testing.T) {
	actions := []model.OperationAction{
		{Type: model.OpViewMetric, TimestampMs: 10000},
		{Type: model.OpModifyConfig, TimestampMs: 20000},
	}

	// 默认阈值 3，1 次观察就变更 → 盲目惩罚
	defaultResult := CalcCostPenalty(actions)
	if defaultResult.BlindPenalty == 0 {
		t.Error("默认配置下应有盲目惩罚")
	}

	// 自定义阈值 1，1 次观察就够了 → 无盲目惩罚
	customCfg := CostPenaltyConfig{
		BlindActionThreshold: 1,
		MaxPenalty:           80,
	}
	customResult := CalcCostPenalty(actions, customCfg)
	if customResult.BlindPenalty != 0 {
		t.Errorf("自定义阈值 1 时不应有盲目惩罚, got %.2f", customResult.BlindPenalty)
	}

	// 自定义配置的惩罚应该更低
	if customResult.Penalty >= defaultResult.Penalty {
		t.Errorf("自定义宽松配置惩罚应更低, default=%.2f, custom=%.2f",
			defaultResult.Penalty, customResult.Penalty)
	}
}

// ========== 工具函数测试 ==========

func TestRepeatMultiplier(t *testing.T) {
	cases := []struct {
		n        int
		expected float64
	}{
		{0, 1.0},
		{1, 1.0},
		{2, 1.0},
		{3, 1.5},
		{4, 2.0},
		{5, 2.5},
		{6, 3.0},
		{10, 3.0}, // 封顶
		{100, 3.0},
	}

	for _, c := range cases {
		got := repeatMultiplier(c.n)
		if got != c.expected {
			t.Errorf("repeatMultiplier(%d) = %.1f, 期望 %.1f", c.n, got, c.expected)
		}
	}
}

func TestOpCategory(t *testing.T) {
	cases := []struct {
		op       model.OperationType
		expected string
	}{
		{model.OpViewMetric, "info"},
		{model.OpViewLog, "info"},
		{model.OpViewConfig, "info"},
		{model.OpViewAlert, "info"},
		{model.OpAIQuestion, "info"},
		{model.OpExecCommand, "diag"},
		{model.OpModifyConfig, "change"},
		{model.OpScaleResource, "change"},
		{model.OpRestartService, "danger"},
		{model.OpFixComplete, "info"},
		{model.OpOther, "diag"},
	}

	for _, c := range cases {
		got := opCategory(c.op)
		if got != c.expected {
			t.Errorf("opCategory(%s) = %s, 期望 %s", c.op, got, c.expected)
		}
	}
}
