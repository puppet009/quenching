package service

import (
	"context"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// T3-11 踩坑/识别判定的编排层测试。
//
// 纯逻辑优先级仲裁已由 pitfall 包 recognition_test.go 覆盖；本文件聚焦编排契约：
//   - 反应轮（坑点注入之后的用户回合）才判定，注入当轮不自判；
//   - 判定结果回写 ai_messages.user_response_type 并维护识别/踩坑计数；
//   - pending 跨轮保留，下一轮继续参与判定；
//   - 任何基础设施失败只静默降级，绝不阻断坑点注入与 LLM 对话。

// reactionConfig 带识别/踩坑规则的坑点配置（触发条件本身不会命中，
// 反应轮靠播种的 pending 消息驱动判定，避免反应轮又注入新坑干扰计数）。
func reactionConfig(id int64) *model.PitfallConfig {
	return pfConfig(id, 100, func(c *model.PitfallConfig) {
		c.Recognition = model.RecognitionRules{
			DisputeKeywords:   []string{"不对", "为什么", "证据呢", "凭什么"},
			IndependentVerify: []string{"view_metric", "view_log", "exec_command"},
			IgnoreAndProceed:  true,
		}
		c.PitfallRules = model.FallRules{
			AdoptActions:  []string{"restart_service", "modify_config"},
			AdoptKeywords: []string{"好的", "就按你说的", "我执行了"},
		}
		// 反应轮不应再触发注入：缺陷置为未暴露，max_times 仅作双保险。
	})
}

// reactionHarness 组装"坑点已注入、等待用户反应"的现场。
// 返回的 injectedAt 即坑点注入时刻，测试据此布置操作时间线。
func reactionHarness(t *testing.T, cfg *model.PitfallConfig) (*ChatService, *fakeStore, *fakeStreamer, *fakePitfallRuntime, int64, time.Time) {
	t.Helper()
	store := newFakeStore()
	st := llmStreamer()
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{cfg},
		snap:    model.PitfallRuntimeContext{Turn: 2, TriggeredDefects: map[int64]bool{}},
		counts:  map[int64]int{},
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID := startTrainingConv(t, svc, 7)

	injectedAt := time.Now().Add(-time.Minute)
	pf := store.seedPendingPitfall(convID, cfg.ID, injectedAt)
	// 该坑已注入过 1 次：即便反应轮触发条件意外满足，max_times=1 也拦住重复注入。
	rt.counts[cfg.ID] = 1
	_ = pf
	return svc, store, st, rt, convID, injectedAt
}

func opAt(at time.Time, typ, category string) timedOp {
	return timedOp{at: at, op: model.PitfallOperation{Type: typ, Category: category}}
}

// ---- 踩坑：执行了 AI 推荐的错误操作 → agree/fell ----

func TestReactionAdoptByActionFell(t *testing.T) {
	cfg := reactionConfig(1)
	svc, store, st, rt, convID, at := reactionHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(10*time.Second), "restart_service", "action")}

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "我重启试试"))

	if st.called != 1 || len(evs) != 2 || evs[0].IsPitfall {
		t.Fatalf("反应轮应走普通 LLM 路径且不注入新坑: called=%d evs=%+v", st.called, evs)
	}
	if got := store.pitfalls[0].responseType; got != model.ResponseAgree {
		t.Fatalf("执行错误操作应判 agree，实际 %q", got)
	}
	if store.fallenCount != 1 || store.recognizedCount != 0 {
		t.Fatalf("踩坑计数 fallen=%d recognized=%d", store.fallenCount, store.recognizedCount)
	}
}

// ---- 踩坑（文本）：明确采纳但操作尚未落地，仍判 agree/fell ----

func TestReactionAdoptByKeywordFell(t *testing.T) {
	cfg := reactionConfig(1)
	svc, store, _, rt, convID, _ := reactionHarness(t, cfg)
	rt.ops = nil // 无任何操作，仅回复表态

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "好的，就按你说的做"))

	if got := store.pitfalls[0].responseType; got != model.ResponseAgree {
		t.Fatalf("明确采纳文本应判 agree，实际 %q", got)
	}
	if store.fallenCount != 1 {
		t.Fatalf("应计 1 次踩坑，实际 %d", store.fallenCount)
	}
}

// ---- 识别：先查指标/日志验证，未执行错误操作 → verified/recognized ----

func TestReactionVerifiedRecognized(t *testing.T) {
	cfg := reactionConfig(1)
	svc, store, _, rt, convID, at := reactionHarness(t, cfg)
	rt.ops = []timedOp{
		opAt(at.Add(5*time.Second), "view_log", "observe"),
		opAt(at.Add(9*time.Second), "view_metric", "observe"),
	}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我先看看日志和指标"))

	if got := store.pitfalls[0].responseType; got != model.ResponseVerified {
		t.Fatalf("验证后走对应判 verified，实际 %q", got)
	}
	if store.recognizedCount != 1 || store.fallenCount != 0 {
		t.Fatalf("识别计数 recognized=%d fallen=%d", store.recognizedCount, store.fallenCount)
	}
}

// ---- 识别：回复反驳/质疑 → dispute/recognized ----

func TestReactionDisputeRecognized(t *testing.T) {
	cfg := reactionConfig(1)
	svc, store, _, rt, convID, at := reactionHarness(t, cfg)
	// 有一条无关操作（既非验证也非采纳），仅靠质疑文本判定。
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "list_files", "observe")}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "凭什么重启？证据呢"))

	if got := store.pitfalls[0].responseType; got != model.ResponseDispute {
		t.Fatalf("质疑应判 dispute，实际 %q", got)
	}
	if store.recognizedCount != 1 {
		t.Fatalf("应计 1 次识别，实际 %d", store.recognizedCount)
	}
}

// ---- 识别：未采纳、沿其他方向排查（有独立操作）→ ignore/recognized ----

func TestReactionIgnoreAndProceedRecognized(t *testing.T) {
	cfg := reactionConfig(1)
	svc, store, _, rt, convID, at := reactionHarness(t, cfg)
	// 沿别的方向排查：操作不是配置的验证项/采纳项，回复也不表态。
	rt.ops = []timedOp{opAt(at.Add(8*time.Second), "tcpdump_capture", "analyze")}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我查下网络链路"))

	if got := store.pitfalls[0].responseType; got != model.ResponseIgnore {
		t.Fatalf("绕行排查应判 ignore，实际 %q", got)
	}
	if store.recognizedCount != 1 {
		t.Fatalf("应计 1 次识别，实际 %d", store.recognizedCount)
	}
}

// ask_ai 不算绕行：只有继续问 AI、无其他信号时保持 pending。
func TestReactionAskAIStaysPending(t *testing.T) {
	cfg := reactionConfig(1)
	svc, store, _, rt, convID, at := reactionHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(3*time.Second), "ask_ai", "ai_assist")}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "还有别的可能吗"))

	if got := store.pitfalls[0].responseType; got != "" {
		t.Fatalf("仅继续问 AI 应保持 pending，实际 %q", got)
	}
	if store.fallenCount != 0 || store.recognizedCount != 0 {
		t.Fatalf("pending 不应计数: fallen=%d recognized=%d", store.fallenCount, store.recognizedCount)
	}
}

// ---- pending 跨轮：本轮无信号不回写，下一轮出现采纳动作才判 fell ----

func TestReactionPendingResolvesNextTurn(t *testing.T) {
	cfg := reactionConfig(1)
	svc, store, _, rt, convID, at := reactionHarness(t, cfg)
	rt.ops = nil

	// 第一轮反应：无表态、无操作 → 保持 pending。
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "嗯"))
	if got := store.pitfalls[0].responseType; got != "" {
		t.Fatalf("首轮无信号应 pending，实际 %q", got)
	}
	if store.fallenCount != 0 {
		t.Fatalf("pending 轮不应计踩坑")
	}

	// 第二轮反应：执行了错误操作 → 补判 agree/fell，只计一次。
	rt.ops = []timedOp{opAt(at.Add(2*time.Minute), "restart_service", "action")}
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))
	if got := store.pitfalls[0].responseType; got != model.ResponseAgree {
		t.Fatalf("次轮执行错误操作应补判 agree，实际 %q", got)
	}
	if store.fallenCount != 1 || store.recognizedCount != 0 {
		t.Fatalf("跨轮判定只应计一次: fallen=%d recognized=%d", store.fallenCount, store.recognizedCount)
	}
}

// ---- 操作时间线的 since 必须是坑点注入时刻：注入前的旧操作不得用于判定 ----

func TestReactionOperationsSinceUsesInjectionTime(t *testing.T) {
	cfg := reactionConfig(1)
	svc, _, _, rt, convID, at := reactionHarness(t, cfg)
	// 只有注入"之前"的重启操作，严格晚于注入时刻的操作为空。
	rt.ops = []timedOp{opAt(at.Add(-30*time.Second), "restart_service", "action")}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "好的"))

	if !rt.opsSince.Equal(at) {
		t.Fatalf("OperationsSince 的 since 应为坑点注入时刻 %v，实际 %v", at, rt.opsSince)
	}
	// 旧操作不可见 + "好的"命中采纳文本 → 仍判 agree，但依据是文本而非旧操作。
	// 这里主要断言 since 边界：若实现误用全量操作，时序断言即已拦住。
}

// ---- 注入当轮不自判：刚注入的坑不能被触发它的那句提问判定 ----

func TestReactionNotJudgedOnInjectionTurn(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	cfg := pfConfig(1, 100, func(c *model.PitfallConfig) {
		c.Recognition = model.RecognitionRules{
			DisputeKeywords:   []string{"为什么"},
			IndependentVerify: []string{"view_metric"},
		}
		c.PitfallRules = model.FallRules{AdoptKeywords: []string{"好的"}}
	})
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{cfg},
		snap:    triggeredSnap(), // 缺陷已暴露 + always/p=1 → 本轮必注入
		counts:  map[int64]int{},
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID := startTrainingConv(t, svc, 7)

	// 触发坑点的提问里即便带质疑词，也绝不能在注入同轮被判 dispute。
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "为什么会这样，好的"))

	if store.pfCount != 1 {
		t.Fatalf("本轮应注入 1 条坑点，实际 %d", store.pfCount)
	}
	if got := store.pitfalls[0].responseType; got != "" {
		t.Fatalf("注入当轮不应自判，实际被写成 %q", got)
	}
	if store.recognizedCount != 0 || store.fallenCount != 0 {
		t.Fatalf("注入当轮不应计数: recognized=%d fallen=%d", store.recognizedCount, store.fallenCount)
	}
}

// ---- 基础设施降级：任一依赖失败都静默回退，对话正常结束 ----

func TestReactionInfraFailuresNeverBlock(t *testing.T) {
	for name, mutate := range map[string]func(*fakeStore, *fakePitfallRuntime){
		"待判定读取失败": func(s *fakeStore, rt *fakePitfallRuntime) { s.failOn = "list_pending" },
		"操作时间线失败": func(_ *fakeStore, rt *fakePitfallRuntime) { rt.failOps = true },
		"回写失败":    func(s *fakeStore, _ *fakePitfallRuntime) { s.failOn = "mark_response" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := reactionConfig(1)
			svc, store, st, rt, convID, at := reactionHarness(t, cfg)
			rt.ops = []timedOp{opAt(at.Add(2*time.Second), "restart_service", "action")}
			mutate(store, rt)

			evs := collect(svc.StreamReply(context.Background(), 7, convID, "好的我执行了"))

			if st.called != 1 {
				t.Fatalf("%s：仍应正常调用 LLM，实际 %d 次", name, st.called)
			}
			if last := evs[len(evs)-1]; last.Type != StreamDone || last.IsPitfall {
				t.Fatalf("%s：应以普通 done 结束: %+v", name, evs)
			}
			// "回写失败"场景下计数不应被污染；其余两场景本就不产生计数（回写根本没发生）。
			if name == "回写失败" && (store.fallenCount != 0 || store.recognizedCount != 0) {
				t.Fatalf("%s：回写失败不应计数", name)
			}
		})
	}
}

// ---- 非训练对话：无 session，根本不进入反应判定 ----

func TestReactionSkippedForNonTrainingConversation(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	cfg := reactionConfig(1)
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{cfg},
		snap:    triggeredSnap(),
		counts:  map[int64]int{},
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID, err := svc.StartConversation(context.Background(), 7, nil, model.ConvKnowledgeQA)
	if err != nil {
		t.Fatal(err)
	}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "好的就按你说的"))

	if rt.opsHits != 0 {
		t.Fatal("非训练对话不应读取操作时间线")
	}
	if store.fallenCount != 0 || store.recognizedCount != 0 {
		t.Fatalf("非训练对话不应有坑点计数: fallen=%d recognized=%d",
			store.fallenCount, store.recognizedCount)
	}
}
