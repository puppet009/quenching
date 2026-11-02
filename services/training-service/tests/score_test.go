package tests

import (
	"math"
	"testing"

	"github.com/quenching/training-service/internal/service"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.5 }

func TestResolvedScoresHigherThanUnresolved(t *testing.T) {
	w := service.DefaultWeights()
	base := service.TechInput{DurationSec: 300, ExpectedSec: 300, CostPenalty: 0, HintCount: 0}

	resolved := base
	resolved.FaultResolved = true
	unresolved := base
	unresolved.FaultResolved = false

	if got := service.TechScore(resolved, w); !approx(got, 100) {
		t.Errorf("完美修复应接近100，实际 %.1f", got)
	}
	// 未修复基础分40（权重0.5），其他维度满分 -> 40*0.5 + 100*0.5 = 70
	if got := service.TechScore(unresolved, w); !approx(got, 70) {
		t.Errorf("未修复但操作干净应约70，实际 %.1f", got)
	}
	if service.TechScore(resolved, w) <= service.TechScore(unresolved, w) {
		t.Error("修复得分必须高于未修复")
	}
}

func TestDurationScore(t *testing.T) {
	cases := []struct {
		name             string
		duration, expect int
		want             float64
	}{
		{"提前完成", 150, 300, 100},
		{"刚好", 300, 300, 100},
		{"1.5倍", 450, 300, 50},
		{"2倍", 600, 300, 0},
		{"超过2倍", 900, 300, 0},
		{"无基准给中性", 999, 0, 60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := service.TechInput{DurationSec: c.duration, ExpectedSec: c.expect}
			if got := service.DurationScore(in); !approx(got, c.want) {
				t.Errorf("got %.1f want %.1f", got, c.want)
			}
		})
	}
}

func TestCostAndHintPenalty(t *testing.T) {
	w := service.DefaultWeights()
	clean := service.TechInput{FaultResolved: true, DurationSec: 300, ExpectedSec: 300, CostPenalty: 0, HintCount: 0}
	costly := clean
	costly.CostPenalty = 50
	withHints := clean
	withHints.HintCount = 3 // 扣24分提示维度

	sClean := service.TechScore(clean, w)
	sCostly := service.TechScore(costly, w)
	sHints := service.TechScore(withHints, w)

	if sCostly >= sClean {
		t.Error("高代价操作应扣分")
	}
	if sHints >= sClean {
		t.Error("多用提示应扣分")
	}
	// 代价50 -> cost维度=50，比满分少50*0.2权重=10分
	if !approx(sClean-sCostly, 10) {
		t.Errorf("代价扣分应约10，实际差 %.1f", sClean-sCostly)
	}
}

func TestCollabWeightedMerge(t *testing.T) {
	w := service.DefaultWeights()
	// 全满
	high := service.CollabSubScores{QuestionQuality: 100, InfoUtilization: 100, Judgment: 100, Available: true}
	if got, ok := service.CollabScore(high, w); !ok || !approx(got, 100) {
		t.Errorf("全满应100，got %.1f ok=%v", got, ok)
	}
	// 判断力权重0.4最高：判断力0应比提问质量0扣更多
	noJudge := service.CollabSubScores{QuestionQuality: 100, InfoUtilization: 100, Judgment: 0, Available: true}
	noQuestion := service.CollabSubScores{QuestionQuality: 0, InfoUtilization: 100, Judgment: 100, Available: true}
	nj, _ := service.CollabScore(noJudge, w)
	nq, _ := service.CollabScore(noQuestion, w)
	if nj >= nq {
		t.Errorf("判断力缺失(%.0f)应比提问缺失(%.0f)扣分更多", nj, nq)
	}
}

func TestNoCollabForManualMode(t *testing.T) {
	w := service.DefaultWeights()
	techIn := service.TechInput{FaultResolved: true, DurationSec: 300, ExpectedSec: 300}
	manual := service.CollabSubScores{Available: false} // A0 纯人工

	res := service.Evaluate(techIn, manual, w)
	if res.HasCollab {
		t.Error("纯人工不应有协作分")
	}
	if res.Collab != 0 {
		t.Errorf("纯人工协作分应为0，实际 %.1f", res.Collab)
	}
	// 综合分应等于技术分，不被空协作分拉低
	if !approx(res.Overall, res.Tech) {
		t.Errorf("纯人工综合分应=技术分，overall %.1f tech %.1f", res.Overall, res.Tech)
	}
	if res.Level != "C1" {
		t.Errorf("无协作数据等级应为C1，实际 %s", res.Level)
	}
}

func TestOverallBlendsBothDimensions(t *testing.T) {
	w := service.DefaultWeights()
	techIn := service.TechInput{FaultResolved: true, DurationSec: 300, ExpectedSec: 300} // tech≈100
	collab := service.CollabSubScores{QuestionQuality: 50, InfoUtilization: 50, Judgment: 50, Available: true}

	res := service.Evaluate(techIn, collab, w)
	if !res.HasCollab {
		t.Fatal("应有协作分")
	}
	if !approx(res.Collab, 50) {
		t.Errorf("协作分应50，实际 %.1f", res.Collab)
	}
	// overall = 100*0.6 + 50*0.4 = 80
	if !approx(res.Overall, 80) {
		t.Errorf("综合分应80，实际 %.1f", res.Overall)
	}
}

func TestCollabLevelMapping(t *testing.T) {
	cases := []struct {
		score float64
		level string
	}{
		{90, "C5"}, {85, "C5"}, {75, "C4"}, {60, "C3"}, {45, "C2"}, {20, "C1"},
	}
	w := service.DefaultWeights()
	for _, c := range cases {
		sub := service.CollabSubScores{QuestionQuality: c.score, InfoUtilization: c.score, Judgment: c.score, Available: true}
		res := service.Evaluate(service.TechInput{FaultResolved: true}, sub, w)
		if res.Level != c.level {
			t.Errorf("协作分%.0f 应映射 %s，实际 %s", c.score, c.level, res.Level)
		}
	}
}

func TestScoresAlwaysWithinRange(t *testing.T) {
	w := service.DefaultWeights()
	// 极端输入：超长时间、巨额代价、超多提示、越界子分
	techIn := service.TechInput{FaultResolved: false, DurationSec: 999999, ExpectedSec: 1, CostPenalty: 9999, HintCount: 999}
	collab := service.CollabSubScores{QuestionQuality: -50, InfoUtilization: 200, Judgment: -10, Available: true}
	res := service.Evaluate(techIn, collab, w)
	for _, v := range []float64{res.Tech, res.Collab, res.Overall} {
		if v < 0 || v > 100 {
			t.Errorf("分数越界: %+v", res)
		}
	}
}

// ——— T3-24 雷达数据 ———

func TestBuildCollabRadarDimensions(t *testing.T) {
	in := service.CollabRadarInput{
		QuestionQuality:   80,
		InfoUtilization:   70,
		Judgment:          90,
		PitfallAwareness:  75,
		VerificationHabit: 65,
		Independence:      85,
	}
	radar := service.BuildCollabRadar(in)

	if len(radar.Dimensions) != 6 {
		t.Fatalf("协作雷达应有6维，实际 %d", len(radar.Dimensions))
	}
	if len(radar.Values) != 6 {
		t.Fatalf("协作雷达值应有6个，实际 %d", len(radar.Values))
	}
	// 维度顺序固定
	expectedDims := []string{"question_quality", "info_utilization", "judgment", "pitfall_awareness", "verification_habit", "independence"}
	for i, d := range expectedDims {
		if radar.Dimensions[i] != d {
			t.Errorf("第%d维应为 %s，实际 %s", i, d, radar.Dimensions[i])
		}
	}
	// 值与输入对应
	if !approx(radar.Values[0], 80) {
		t.Errorf("提问质量应80，实际 %.1f", radar.Values[0])
	}
	if !approx(radar.Values[2], 90) {
		t.Errorf("判断力应90，实际 %.1f", radar.Values[2])
	}
}

func TestBuildCollabRadarClamp(t *testing.T) {
	in := service.CollabRadarInput{
		QuestionQuality:   -10,
		InfoUtilization:   200,
		Judgment:          150,
		PitfallAwareness:  -5,
		VerificationHabit: 0,
		Independence:      100,
	}
	radar := service.BuildCollabRadar(in)
	for i, v := range radar.Values {
		if v < 0 || v > 100 {
			t.Errorf("第%d维值越界: %.1f", i, v)
		}
	}
}

func TestBuildDualRadarMatchesResult(t *testing.T) {
	w := service.DefaultWeights()
	techIn := service.TechInput{FaultResolved: true, DurationSec: 300, ExpectedSec: 300}
	collabSub := service.CollabSubScores{QuestionQuality: 80, InfoUtilization: 70, Judgment: 90, Available: true}

	res := service.Evaluate(techIn, collabSub, w)
	techRadar := service.BuildTechRadar(techIn, w)
	collabRadar := service.BuildCollabRadar(service.CollabRadarInput{
		QuestionQuality: 80, InfoUtilization: 70, Judgment: 90,
	})

	dual := service.BuildDualRadar(techRadar, collabRadar, res)

	if !approx(dual.TechOverall, res.Tech) {
		t.Errorf("技术总分不匹配: dual=%.1f res=%.1f", dual.TechOverall, res.Tech)
	}
	if !approx(dual.CollabOverall, res.Collab) {
		t.Errorf("协作总分不匹配: dual=%.1f res=%.1f", dual.CollabOverall, res.Collab)
	}
	if dual.Level != res.Level {
		t.Errorf("等级不匹配: dual=%s res=%s", dual.Level, res.Level)
	}
	if !dual.HasCollab {
		t.Error("应有协作数据")
	}
}

func TestTechRadarHasSixDimensions(t *testing.T) {
	w := service.DefaultWeights()
	techIn := service.TechInput{FaultResolved: true, DurationSec: 300, ExpectedSec: 300, CostPenalty: 0, HintCount: 0}
	radar := service.BuildTechRadar(techIn, w)

	if len(radar.Dimensions) != 6 {
		t.Fatalf("技术雷达应有6维，实际 %d", len(radar.Dimensions))
	}
	if len(radar.Values) != 6 {
		t.Fatalf("技术雷达值应有6个，实际 %d", len(radar.Values))
	}
	// 所有值应在 0-100 之间
	for i, v := range radar.Values {
		if v < 0 || v > 100 {
			t.Errorf("技术雷达第%d维越界: %.1f", i, v)
		}
	}
}

func TestDualRadarNoCollab(t *testing.T) {
	w := service.DefaultWeights()
	techIn := service.TechInput{FaultResolved: true, DurationSec: 300, ExpectedSec: 300}
	collabSub := service.CollabSubScores{Available: false}

	res := service.Evaluate(techIn, collabSub, w)
	techRadar := service.BuildTechRadar(techIn, w)
	collabRadar := service.BuildCollabRadar(service.CollabRadarInput{})

	dual := service.BuildDualRadar(techRadar, collabRadar, res)

	if dual.HasCollab {
		t.Error("纯人工模式 HasCollab 应为 false")
	}
	// 综合分应等于技术分
	if !approx(dual.Overall, dual.TechOverall) {
		t.Errorf("无协作时综合分应=技术分，overall=%.1f tech=%.1f", dual.Overall, dual.TechOverall)
	}
}
