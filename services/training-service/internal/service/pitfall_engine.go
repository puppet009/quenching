package service

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"

	"github.com/quenching/training-service/internal/model"
)

// RollSource 概率判定的随机源。显式注入而非直接用全局 rand，
// 使"概率触发"可在测试中确定性复现（生产用 math/rand，测试用固定序列）。
type RollSource interface {
	// Float64 返回 [0,1) 的伪随机数。
	Float64() float64
}

// Engine 规则版坑点触发判定引擎（T3-9）。
//
// 无状态：引擎自身不记录"已注入次数"，次数由编排层按会话从 State 统计后传入，
// 因此引擎可被多个会话并发复用，也天然可单测。配置在训练启动时由 Loader 加载。
type Engine struct {
	roll RollSource
}

// NewEngine 创建判定引擎；roll 传 nil 时使用默认 math/rand 源。
func NewEngine(roll RollSource) *Engine {
	if roll == nil {
		roll = rand.New(rand.NewSource(rand.Int63()))
	}
	return &Engine{roll: roll}
}

// Decision 一条配置的判定结果。
type Decision struct {
	Config *model.PitfallConfig
	// Fired 本判定时刻是否应当注入该坑点。
	Fired bool
	// Reason 未命中原因（命中为空），便于日志/排查与 T3-11 复用。
	Reason string
}

// Evaluate 针对"某条缺陷的一组有序坑点配置"在当前上下文下逐条判定。
//
// 返回每条配置的判定结果（顺序与输入一致）。同一时刻至多注入一个坑点由
// PickFirstFired 决定（见下），这里先把全部判定算清楚，便于测试覆盖。
func (e *Engine) Evaluate(cfgs []*model.PitfallConfig, st State, ctx model.PitfallRuntimeContext) []Decision {
	out := make([]Decision, 0, len(cfgs))
	for _, c := range cfgs {
		fired, reason := e.evaluateOne(c, st, ctx)
		out = append(out, Decision{Config: c, Fired: fired, Reason: reason})
	}
	return out
}

// PickFirstFired 在判定结果中返回第一个命中的坑点（配置已按 sort_order,id 排序，
// 与后台配置的触发优先级一致）。无命中返回 nil。
func PickFirstFired(ds []Decision) *model.PitfallConfig {
	for i := range ds {
		if ds[i].Fired {
			return ds[i].Config
		}
	}
	return nil
}

// evaluateOne 单条配置判定：前置状态 -> 时机 -> 次数上限 -> 概率，顺序短路。
func (e *Engine) evaluateOne(c *model.PitfallConfig, st State, ctx model.PitfallRuntimeContext) (bool, string) {
	if reason := checkPrerequisites(c, st, ctx); reason != "" {
		return false, reason
	}
	if reason := checkTiming(c, ctx); reason != "" {
		return false, reason
	}
	// 次数上限：一次训练内同一坑点最多注入 max_times 次（缺省 1）。
	if injected := st.InjectedCount(c.ID); injected >= c.Trigger.EffectiveMaxTimes() {
		return false, fmt.Sprintf("坑点 %d 已达注入上限 %d", c.ID, c.Trigger.EffectiveMaxTimes())
	}
	// 概率：时机满足后按 probability 掷骰。probability=1 时确定性必中（验收口径）。
	p := c.Trigger.EffectiveProbability()
	if p >= 1 {
		return true, ""
	}
	if p <= 0 {
		return false, fmt.Sprintf("坑点 %d 概率为 0", c.ID)
	}
	if e.roll.Float64() >= p {
		return false, fmt.Sprintf("坑点 %d 概率掷骰未中 (p=%.2f)", c.ID, p)
	}
	return true, ""
}

// checkPrerequisites 前置状态：该坑点所属故障是否已触发、轮次窗口。
func checkPrerequisites(c *model.PitfallConfig, _ State, ctx model.PitfallRuntimeContext) string {
	r := c.Trigger.Requires
	if r == nil {
		// 未配置前置时，仍默认要求该坑点所属故障已触发（见 TriggerCondition.RequiresFaultTriggered）。
		if c.Trigger.RequiresFaultTriggered() && !ctx.TriggeredDefects[c.DefectID] {
			return fmt.Sprintf("坑点 %d 前置未满足：缺陷 %d 尚未触发", c.ID, c.DefectID)
		}
		return ""
	}
	if c.Trigger.RequiresFaultTriggered() && !ctx.TriggeredDefects[c.DefectID] {
		return fmt.Sprintf("坑点 %d 前置未满足：缺陷 %d 尚未触发", c.ID, c.DefectID)
	}
	if r.MinTurn != 0 && ctx.Turn < r.MinTurn {
		return fmt.Sprintf("坑点 %d 前置未满足：当前第 %d 轮 < min_turn %d", c.ID, ctx.Turn, r.MinTurn)
	}
	if r.MaxTurn != 0 && ctx.Turn > r.MaxTurn {
		return fmt.Sprintf("坑点 %d 前置未满足：当前第 %d 轮 > max_turn %d", c.ID, ctx.Turn, r.MaxTurn)
	}
	return ""
}

// checkTiming 时机判定：always / question_match / stage_enter / delay。
func checkTiming(c *model.PitfallConfig, ctx model.PitfallRuntimeContext) string {
	switch c.Trigger.EffectiveWhen() {
	case model.WhenAlways:
		return ""

	case model.WhenQuestionMatch:
		if !matchQuestion(c.Trigger, ctx.Question) {
			return fmt.Sprintf("坑点 %d 时机未到：提问未命中关键词/模式", c.ID)
		}
		return ""

	case model.WhenStageEnter:
		if ctx.Stage == "" || !containsString(c.Trigger.Stages, ctx.Stage) {
			return fmt.Sprintf("坑点 %d 时机未到：未进入指定阶段", c.ID)
		}
		return ""

	case model.WhenDelay:
		t := c.Trigger
		if t.Window != nil {
			if ctx.ElapsedSec < t.Window.StartSeconds || ctx.ElapsedSec >= t.Window.EndSeconds {
				return fmt.Sprintf("坑点 %d 时机未到：%d 秒不在窗口 [%d,%d)",
					c.ID, ctx.ElapsedSec, t.Window.StartSeconds, t.Window.EndSeconds)
			}
			return ""
		}
		// 固定延迟：达到 delay_seconds 即视为时机到来（之后持续有效，直到次数耗尽）。
		if ctx.ElapsedSec < t.DelaySeconds {
			return fmt.Sprintf("坑点 %d 时机未到：%d 秒 < delay %d", c.ID, ctx.ElapsedSec, t.DelaySeconds)
		}
		return ""

	default:
		return fmt.Sprintf("坑点 %d 不支持的触发时机 %q", c.ID, c.Trigger.When)
	}
}

// matchQuestion 用户提问命中任一关键词（子串、大小写不敏感）或任一正则模式。
func matchQuestion(t model.TriggerCondition, question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return false
	}
	for _, kw := range t.QuestionKeywords {
		kw = strings.TrimSpace(kw)
		if kw != "" && strings.Contains(q, strings.ToLower(kw)) {
			return true
		}
	}
	for _, pat := range t.QuestionPatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			// 配置校验阶段本应拦下非法正则；此处防御性跳过，避免单条坏配置打挂训练。
			continue
		}
		if re.MatchString(question) {
			return true
		}
	}
	return false
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
