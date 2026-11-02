package service

import (
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// ---- 测试夹具 ----

// fixedRoll 固定概率随机源：返回值恒定，用于确定性复现掷骰结果。
type fixedRoll struct{ v float64 }

func (r fixedRoll) Float64() float64 { return r.v }

// testCfg 构造一条默认"即可命中"的配置：always / p=1 / max_times=1 / 属缺陷 100。
// 各用例按需要 mutate；默认前置要求缺陷 100 已触发（缺省口径）。
func testCfg(id int64, mutate ...func(*model.PitfallConfig)) *model.PitfallConfig {
	c := &model.PitfallConfig{
		ID:              id,
		DefectID:        100,
		PitfallType:     model.TypeCausalMisjudgment,
		Name:            "测试坑点",
		ErrorContent:    "建议直接重启服务",
		ConfidenceLevel: model.ConfidenceHigh,
		SortOrder:       int(id),
		Status:          model.StatusActive,
		Trigger: model.TriggerCondition{
			When:        model.WhenAlways,
			Probability: 1,
			MaxTimes:    1,
		},
	}
	for _, m := range mutate {
		m(c)
	}
	return c
}

func baseCtx() model.PitfallRuntimeContext {
	return model.PitfallRuntimeContext{
		Turn:             1,
		Question:         "现在该怎么办",
		TriggeredDefects: map[int64]bool{100: true},
	}
}

func firedCount(ds []Decision) int {
	n := 0
	for _, d := range ds {
		if d.Fired {
			n++
		}
	}
	return n
}

// ---- 前置判定 ----

func TestEvaluateFiresWithFaultTriggered(t *testing.T) {
	e := NewEngine(fixedRoll{0.1})
	ds := e.Evaluate([]*model.PitfallConfig{testCfg(1)}, NewState(), baseCtx())
	if len(ds) != 1 || !ds[0].Fired || ds[0].Reason != "" {
		t.Fatalf("前置满足应命中: %+v", ds)
	}
}

func TestEvaluateBlocksBeforeFaultTriggered(t *testing.T) {
	e := NewEngine(fixedRoll{0.1})
	ctx := baseCtx()
	ctx.TriggeredDefects = map[int64]bool{} // 缺陷 100 尚未触发

	ds := e.Evaluate([]*model.PitfallConfig{testCfg(1)}, NewState(), ctx)
	if ds[0].Fired {
		t.Fatal("故障未触发时不应注入坑点")
	}
	if ds[0].Reason == "" {
		t.Fatal("未命中应给出原因")
	}
}

func TestEvaluatePrereqScopedByDefect(t *testing.T) {
	// 一次会话挂两个缺陷：只有 100 暴露，缺陷 200 的坑不得提前出现。
	c100 := testCfg(1)
	c200 := testCfg(2, func(c *model.PitfallConfig) { c.DefectID = 200 })
	e := NewEngine(fixedRoll{0.1})
	ctx := baseCtx()

	ds := e.Evaluate([]*model.PitfallConfig{c100, c200}, NewState(), ctx)
	if !ds[0].Fired || ds[1].Fired {
		t.Fatalf("应仅命中缺陷100的坑: %+v", ds)
	}

	ctx.TriggeredDefects[200] = true
	ds = e.Evaluate([]*model.PitfallConfig{c100, c200}, NewState(), ctx)
	if !ds[0].Fired || !ds[1].Fired {
		t.Fatalf("两缺陷均暴露后两个坑都应可命中: %+v", ds)
	}
}

func TestEvaluateFaultTriggeredExplicitFalse(t *testing.T) {
	noFault := false
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.Requires = &model.TriggerRequires{FaultTriggered: &noFault}
	})
	ctx := baseCtx()
	ctx.TriggeredDefects = map[int64]bool{}

	ds := NewEngine(fixedRoll{0.1}).Evaluate([]*model.PitfallConfig{c}, NewState(), ctx)
	if !ds[0].Fired {
		t.Fatalf("显式 fault_triggered=false 时不应拦截: %+v", ds)
	}
}

func TestEvaluateTurnWindow(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.Requires = &model.TriggerRequires{MinTurn: 2, MaxTurn: 3}
	})
	e := NewEngine(fixedRoll{0.1})

	for turn, want := range map[int]bool{1: false, 2: true, 3: true, 4: false} {
		ctx := baseCtx()
		ctx.Turn = turn
		ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), ctx)
		if ds[0].Fired != want {
			t.Fatalf("第 %d 轮期望 fired=%v，实际 reason=%q", turn, want, ds[0].Reason)
		}
	}
}

// ---- 时机判定 ----

func TestTimingQuestionMatch(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenQuestionMatch
		c.Trigger.QuestionKeywords = []string{"重启", "CPU"}
	})
	e := NewEngine(fixedRoll{0.1})

	hit := baseCtx()
	hit.Question = "要不要重启一下服务？"
	miss := baseCtx()
	miss.Question = "帮我看看日志"
	upper := baseCtx() // 关键词大小写不敏感
	upper.Question = "cpu 占用一直很高"

	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), hit); !ds[0].Fired {
		t.Fatal("命中关键词应触发")
	}
	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), miss); ds[0].Fired {
		t.Fatal("未命中关键词不应触发")
	}
	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), upper); !ds[0].Fired {
		t.Fatal("关键词匹配应大小写不敏感")
	}
}

func TestTimingQuestionRegexp(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenQuestionMatch
		c.Trigger.QuestionPatterns = []string{`为什么.*高`}
	})
	e := NewEngine(fixedRoll{0.1})

	hit := baseCtx()
	hit.Question = "为什么负载这么高"
	empty := baseCtx()
	empty.Question = "   "

	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), hit); !ds[0].Fired {
		t.Fatal("正则命中应触发")
	}
	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), empty); ds[0].Fired {
		t.Fatal("空提问不应触发")
	}
}

func TestTimingQuestionBadRegexpDefensive(t *testing.T) {
	// 非法正则本应在配置校验阶段被拦；引擎层防御性跳过，且关键词命中仍应触发，不得 panic。
	withKeyword := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenQuestionMatch
		c.Trigger.QuestionPatterns = []string{"[invalid"}
		c.Trigger.QuestionKeywords = []string{"重启"}
	})
	onlyBadPattern := testCfg(2, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenQuestionMatch
		c.Trigger.QuestionPatterns = []string{"[invalid"}
	})
	e := NewEngine(fixedRoll{0.1})
	ctx := baseCtx()
	ctx.Question = "重启吗"

	if ds := e.Evaluate([]*model.PitfallConfig{withKeyword}, NewState(), ctx); !ds[0].Fired {
		t.Fatal("坏正则不应影响关键词命中")
	}
	ctx2 := baseCtx()
	ctx2.Question = "随便问问"
	if ds := e.Evaluate([]*model.PitfallConfig{onlyBadPattern}, NewState(), ctx2); ds[0].Fired {
		t.Fatal("只有坏正则且无关键词时不应触发")
	}
}

func TestTimingStageEnter(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenStageEnter
		c.Trigger.Stages = []string{"fix"}
	})
	e := NewEngine(fixedRoll{0.1})

	inStage := baseCtx()
	inStage.Stage = "fix"
	otherStage := baseCtx()
	otherStage.Stage = "detect"
	noStage := baseCtx() // 无阶段记录（空串），stage_enter 不得提前命中

	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), inStage); !ds[0].Fired {
		t.Fatal("进入指定阶段应触发")
	}
	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), otherStage); ds[0].Fired {
		t.Fatal("未进入指定阶段不应触发")
	}
	if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), noStage); ds[0].Fired {
		t.Fatal("阶段未知（空串）不应触发")
	}
}

func TestTimingDelayFixed(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenDelay
		c.Trigger.DelaySeconds = 60
	})
	e := NewEngine(fixedRoll{0.1})

	for elapsed, want := range map[int]bool{59: false, 60: true, 120: true} {
		ctx := baseCtx()
		ctx.ElapsedSec = elapsed
		if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), ctx); ds[0].Fired != want {
			t.Fatalf("延迟 %d 秒期望 fired=%v，实际 reason=%q", elapsed, want, ds[0].Reason)
		}
	}
}

func TestTimingDelayWindow(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenDelay
		c.Trigger.Window = &model.TimeWindow{StartSeconds: 30, EndSeconds: 60}
	})
	e := NewEngine(fixedRoll{0.1})

	// 窗口 [30,60)：左闭右开
	for elapsed, want := range map[int]bool{29: false, 30: true, 59: true, 60: false} {
		ctx := baseCtx()
		ctx.ElapsedSec = elapsed
		if ds := e.Evaluate([]*model.PitfallConfig{c}, NewState(), ctx); ds[0].Fired != want {
			t.Fatalf("窗口 %d 秒期望 fired=%v，实际 reason=%q", elapsed, want, ds[0].Reason)
		}
	}
}

// ---- 次数上限 ----

func TestMaxTimes(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) { c.Trigger.MaxTimes = 2 })
	e := NewEngine(fixedRoll{0.1})

	st := NewState()
	if ds := e.Evaluate([]*model.PitfallConfig{c}, st, baseCtx()); !ds[0].Fired {
		t.Fatal("第 1 次应命中")
	}
	st.RecordInjection(1)
	if ds := e.Evaluate([]*model.PitfallConfig{c}, st, baseCtx()); !ds[0].Fired {
		t.Fatal("上限 2 次时第 2 次仍应命中")
	}
	st.RecordInjection(1)
	if ds := e.Evaluate([]*model.PitfallConfig{c}, st, baseCtx()); ds[0].Fired {
		t.Fatal("达上限后不应再命中")
	}
}

func TestMaxTimesDefaultOne(t *testing.T) {
	c := testCfg(1) // MaxTimes 缺省 1
	e := NewEngine(fixedRoll{0.1})
	st := NewState()
	st.RecordInjection(1)

	ds := e.Evaluate([]*model.PitfallConfig{c}, st, baseCtx())
	if ds[0].Fired || ds[0].Reason == "" {
		t.Fatalf("缺省上限 1 次，注入过后不应再命中: %+v", ds)
	}
}

// ---- 概率 ----

func TestProbabilityDeterministic(t *testing.T) {
	// p=1 时无论随机源返回什么都必中——验收口径"符合条件 100% 出现"。
	for _, v := range []float64{0, 0.5, 0.99, 1} {
		c := testCfg(1)
		if ds := NewEngine(fixedRoll{v}).Evaluate([]*model.PitfallConfig{c}, NewState(), baseCtx()); !ds[0].Fired {
			t.Fatalf("p=1 时 roll=%v 也应必中", v)
		}
	}
}

func TestProbabilityRoll(t *testing.T) {
	c := testCfg(1, func(c *model.PitfallConfig) { c.Trigger.Probability = 0.5 })

	// roll < p 中，roll >= p 不中（见 evaluateOne）。
	if ds := NewEngine(fixedRoll{0.49}).Evaluate([]*model.PitfallConfig{c}, NewState(), baseCtx()); !ds[0].Fired {
		t.Fatal("roll=0.49 < p=0.5 应命中")
	}
	if ds := NewEngine(fixedRoll{0.5}).Evaluate([]*model.PitfallConfig{c}, NewState(), baseCtx()); ds[0].Fired {
		t.Fatal("roll=0.5 >= p=0.5 不应命中")
	}
	if ds := NewEngine(fixedRoll{0.9}).Evaluate([]*model.PitfallConfig{c}, NewState(), baseCtx()); ds[0].Fired {
		t.Fatal("roll=0.9 >= p=0.5 不应命中")
	}
}

// ---- 短路顺序：前置未满足时，不进入时机/概率判定 ----

func TestShortCircuitPrereqFirst(t *testing.T) {
	// 前置（缺陷未触发）不满足；即便时机 question_match 也不匹配、概率极低，
	// 未命中原因仍应首先报告前置。
	c := testCfg(1, func(c *model.PitfallConfig) {
		c.Trigger.When = model.WhenQuestionMatch
		c.Trigger.QuestionKeywords = []string{"不可能出现的提问词"}
	})
	ctx := baseCtx()
	ctx.TriggeredDefects = map[int64]bool{}

	ds := NewEngine(fixedRoll{0.1}).Evaluate([]*model.PitfallConfig{c}, NewState(), ctx)
	if ds[0].Fired || ds[0].Reason == "" {
		t.Fatalf("应被前置短路: %+v", ds)
	}
}

// ---- PickFirstFired / 顺序保持 ----

func TestEvaluatePreservesOrder(t *testing.T) {
	c1, c2 := testCfg(10), testCfg(20)
	ds := NewEngine(fixedRoll{0.1}).Evaluate([]*model.PitfallConfig{c1, c2}, NewState(), baseCtx())
	if len(ds) != 2 || ds[0].Config.ID != 10 || ds[1].Config.ID != 20 {
		t.Fatalf("判定结果顺序须与输入一致: %+v", ds)
	}
}

func TestPickFirstFired(t *testing.T) {
	blocked := testCfg(1, func(c *model.PitfallConfig) { c.DefectID = 200 }) // 缺陷 200 未触发 -> 不命中
	hit := testCfg(2)                                                        // 缺陷 100 已触发 -> 命中
	ctx := baseCtx()                                                         // 仅 100 已触发

	ds := NewEngine(fixedRoll{0.1}).Evaluate([]*model.PitfallConfig{blocked, hit}, NewState(), ctx)
	if got := PickFirstFired(ds); got == nil || got.ID != 2 {
		t.Fatalf("应跳过未命中返回第一个命中的配置(2): %v", got)
	}

	ctx.TriggeredDefects = map[int64]bool{}
	ds = NewEngine(fixedRoll{0.1}).Evaluate([]*model.PitfallConfig{blocked, hit}, NewState(), ctx)
	if got := PickFirstFired(ds); got != nil {
		t.Fatalf("全部未命中应返回 nil: %v", got)
	}
	if got := PickFirstFired(nil); got != nil {
		t.Fatal("空判定列表应返回 nil")
	}
}

func TestEvaluateEmpty(t *testing.T) {
	ds := NewEngine(nil).Evaluate(nil, NewState(), baseCtx())
	if len(ds) != 0 {
		t.Fatalf("空配置应返回空结果: %+v", ds)
	}
}

// ---- State ----

func TestStateCounts(t *testing.T) {
	st := NewState()
	if st.InjectedCount(1) != 0 || st.TotalInjected() != 0 {
		t.Fatal("新状态计数应为 0")
	}
	st.RecordInjection(1)
	st.RecordInjection(1)
	st.RecordInjection(2)
	if st.InjectedCount(1) != 2 || st.InjectedCount(2) != 1 || st.TotalInjected() != 3 {
		t.Fatalf("计数错误: c1=%d c2=%d total=%d", st.InjectedCount(1), st.InjectedCount(2), st.TotalInjected())
	}
}

func TestStateNilMapSelfHeal(t *testing.T) {
	// 零值 State 直接使用也不应 panic（map 自愈）。
	var st State
	st.RecordInjection(7)
	if st.InjectedCount(7) != 1 || st.TotalInjected() != 1 {
		t.Fatal("零值 State 应自愈并正常计数")
	}
}
