package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// ---- T3-5 提示配额供给 fake ----

// fakeHintQuotaProvider 可控的配额供给：难度固定，用量从 fakeStore 按会话实时统计
// （仿线上 ai_messages.is_hint 的口径，天然覆盖"跨对话累计"）；也可强制报错验证 fail-closed。
type fakeHintQuotaProvider struct {
	store          *fakeStore
	difficulty     string
	difficultyErr  error
	usageErr       error
	difficultyHits int
	usageHits      int
}

func (p *fakeHintQuotaProvider) Difficulty(_ context.Context, _ int64) (string, error) {
	p.difficultyHits++
	if p.difficultyErr != nil {
		return "", p.difficultyErr
	}
	return p.difficulty, nil
}

func (p *fakeHintQuotaProvider) Usage(_ context.Context, sessionID int64) (HintUsage, error) {
	p.usageHits++
	if p.usageErr != nil {
		return HintUsage{}, p.usageErr
	}
	return p.store.hintUsage(sessionID), nil
}

// hintHarness 装配一个启用提示能力的训练对话（默认 easy：8 次 / 3 级）。
func hintHarness(t *testing.T, difficultyLevel string) (*ChatService, *fakeStore, *fakeStreamer, *fakeHintQuotaProvider, int64) {
	t.Helper()
	store := newFakeStore()
	st := newStreamerWithDone()
	provider := &fakeHintQuotaProvider{store: store, difficulty: difficultyLevel}
	svc := NewChatService(store, st, "mock", "")
	svc.EnableHints(provider)
	const sessionID int64 = 700
	convID := startTrainingConvWithSession(t, svc, 7, sessionID)
	return svc, store, st, provider, convID
}

func onlyError(evs []StreamEvent) string {
	if len(evs) != 1 || evs[0].Type != StreamError {
		return ""
	}
	return evs[0].ErrorMsg
}

// ---- 逐级解锁 ----

// easy 首次只能要 1 级；按 1→2→3 顺序全部放行，且剩余次数逐次递减。
func TestHintProgressiveUnlockEasy(t *testing.T) {
	svc, store, st, _, convID := hintHarness(t, "easy")

	// 首次直接要 2 级：越级，拒绝。
	msg := onlyError(collect(svc.StreamReply(context.Background(), 7, convID, "提示", WithHintLevel(2))))
	if !strings.Contains(msg, ErrHintLevelLocked.Error()) {
		t.Fatalf("首次请求 2 级应被逐级解锁拒绝，实际: %q", msg)
	}
	if !strings.Contains(msg, "上一级") {
		t.Fatalf("错误文案应提示先解锁上一级，实际: %s", msg)
	}
	if store.upCount != 0 || store.hintCount != 0 {
		t.Fatalf("被拒请求不应落库: user=%d hint=%d", store.upCount, store.hintCount)
	}

	// 1 级放行，剩余 8-0-1=7。
	evs := collect(svc.StreamReply(context.Background(), 7, convID, "给个方向", WithHintLevel(1)))
	if last := evs[len(evs)-1]; last.Type != StreamDone || last.HintLevel != 1 || last.HintRemaining != 7 {
		t.Fatalf("1 级提示 done 帧异常: %+v", evs)
	}
	if st.got.HintLevel != 1 || st.got.HintRemaining != 7 {
		t.Fatalf("发给 AI 的提示入参异常: level=%d remaining=%d", st.got.HintLevel, st.got.HintRemaining)
	}

	// 2 级放行，剩余 6。
	evs = collect(svc.StreamReply(context.Background(), 7, convID, "再具体点", WithHintLevel(2)))
	if last := evs[len(evs)-1]; last.Type != StreamDone || last.HintLevel != 2 || last.HintRemaining != 6 {
		t.Fatalf("2 级提示 done 帧异常: %+v", evs)
	}
	// 跳级要 3 之前已用过 2，3 级放行，剩余 5。
	evs = collect(svc.StreamReply(context.Background(), 7, convID, "接近答案了吗", WithHintLevel(3)))
	if last := evs[len(evs)-1]; last.Type != StreamDone || last.HintLevel != 3 || last.HintRemaining != 5 {
		t.Fatalf("3 级提示 done 帧异常: %+v", evs)
	}
	if store.hintCount != 3 {
		t.Fatalf("应落库 3 条提示消息，实际: %d", store.hintCount)
	}
	if got := store.hints; got[0].level != 1 || got[1].level != 2 || got[2].level != 3 {
		t.Fatalf("提示落库级别顺序错误: %+v", got)
	}
}

// 已用最高级 2 之后，再要 1 级永远允许（不超过 MaxLevel+1），不强制顺序递增。
func TestHintLowerLevelAlwaysAllowed(t *testing.T) {
	svc, _, _, _, convID := hintHarness(t, "easy")
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "1", WithHintLevel(1)))
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "2", WithHintLevel(2)))

	// 再回到 1 级：允许。
	evs := collect(svc.StreamReply(context.Background(), 7, convID, "再来个方向", WithHintLevel(1)))
	if last := evs[len(evs)-1]; last.Type != StreamDone || last.HintLevel != 1 {
		t.Fatalf("已解锁高级后低级提示应仍可用: %+v", evs)
	}
}

// ---- 难度级数上限 ----

func TestHintLevelCeilingByDifficulty(t *testing.T) {
	cases := []struct {
		difficulty string
		allowed    int // 该难度最高可用级别
	}{
		{"easy", 3},
		{"normal", 2},
		{"hard", 1},
		{"medium", 2}, // 历史库值，归一为 normal
		{"", 2},       // 空值/未知值走 MustGet 的 normal 回退
	}
	for _, tc := range cases {
		t.Run(tc.difficulty, func(t *testing.T) {
			svc, _, _, _, convID := hintHarness(t, tc.difficulty)
			// 逐级解锁到上限。
			for lvl := 1; lvl <= tc.allowed; lvl++ {
				evs := collect(svc.StreamReply(context.Background(), 7, convID, "问", WithHintLevel(lvl)))
				if last := evs[len(evs)-1]; last.Type != StreamDone || last.HintLevel != lvl {
					t.Fatalf("难度 %q 第 %d 级应放行: %+v", tc.difficulty, lvl, evs)
				}
			}
			// 上限+1 必须拒绝。easy 的上限就是全局最高级 3，第 4 级先撞全局范围校验
			// （ErrHintLevelInvalid），该路径已由 TestHintLevelOutOfRange 覆盖，此处跳过。
			if tc.allowed >= 3 {
				return
			}
			msg := onlyError(collect(svc.StreamReply(
				context.Background(), 7, convID, "问", WithHintLevel(tc.allowed+1))))
			if !errors.Is(errSentinel(msg), ErrHintLevelLocked) {
				t.Fatalf("难度 %q 第 %d 级应被级数上限拒绝，实际: %q", tc.difficulty, tc.allowed+1, msg)
			}
		})
	}
}

// ---- 次数配额 ----

func TestHintQuotaExhaustedByDifficulty(t *testing.T) {
	cases := []struct {
		difficulty string
		quota      int
	}{
		{"easy", 8},
		{"normal", 5},
		{"hard", 3},
	}
	for _, tc := range cases {
		t.Run(tc.difficulty, func(t *testing.T) {
			svc, store, _, _, convID := hintHarness(t, tc.difficulty)
			// hard 只有 1 级；其余难度反复用 1 级即可耗尽，不碰级数限制。
			for i := 0; i < tc.quota; i++ {
				evs := collect(svc.StreamReply(context.Background(), 7, convID, "提示", WithHintLevel(1)))
				if last := evs[len(evs)-1]; last.Type != StreamDone {
					t.Fatalf("第 %d 次提示应放行: %+v", i+1, evs)
				}
			}
			msg := onlyError(collect(svc.StreamReply(
				context.Background(), 7, convID, "再提示一次", WithHintLevel(1))))
			if msg != ErrHintExhausted.Error() {
				t.Fatalf("第 %d 次应提示次数耗尽，实际: %q", tc.quota+1, msg)
			}
			if store.hintCount != tc.quota {
				t.Fatalf("应只落库 %d 条提示，实际 %d", tc.quota, store.hintCount)
			}
		})
	}
}

// ---- 跨对话按会话累计：同一会话开第二个对话，配额沿用 ----

func TestHintQuotaAccumulatesAcrossConversations(t *testing.T) {
	svc, store, _, _, conv1 := hintHarness(t, "hard") // hard 配额 3
	const sessionID int64 = 700

	// 第一个对话用掉 2 次。
	_ = collect(svc.StreamReply(context.Background(), 7, conv1, "提示", WithHintLevel(1)))
	_ = collect(svc.StreamReply(context.Background(), 7, conv1, "提示", WithHintLevel(1)))

	// 同会话的第二个对话：只剩 1 次。
	conv2 := startTrainingConvWithSession(t, svc, 7, sessionID)
	evs := collect(svc.StreamReply(context.Background(), 7, conv2, "提示", WithHintLevel(1)))
	if last := evs[len(evs)-1]; last.Type != StreamDone || last.HintRemaining != 0 {
		t.Fatalf("跨对话第 3 次提示剩余应为 0: %+v", evs)
	}
	msg := onlyError(collect(svc.StreamReply(
		context.Background(), 7, conv2, "再来", WithHintLevel(1))))
	if msg != ErrHintExhausted.Error() {
		t.Fatalf("跨对话累计第 4 次应耗尽，实际: %q", msg)
	}
	if store.hintCount != 3 {
		t.Fatalf("两会话合计应落 3 条提示，实际 %d", store.hintCount)
	}

	// 另一个训练会话：配额独立，重新计数。
	convOther := startTrainingConvWithSession(t, svc, 7, 999)
	evs = collect(svc.StreamReply(context.Background(), 7, convOther, "提示", WithHintLevel(1)))
	if last := evs[len(evs)-1]; last.Type != StreamDone {
		t.Fatalf("新会话配额应独立: %+v", evs)
	}
}

// ---- 非训练对话 / 未装配 / 非法级别 ----

func TestHintRejectedForNonTrainingConversation(t *testing.T) {
	store := newFakeStore()
	st := newStreamerWithDone()
	provider := &fakeHintQuotaProvider{store: store, difficulty: "easy"}
	svc := NewChatService(store, st, "mock", "")
	svc.EnableHints(provider)
	convID, _ := svc.StartConversation(context.Background(), 7, nil, model.ConvKnowledgeQA)

	msg := onlyError(collect(svc.StreamReply(
		context.Background(), 7, convID, "提示", WithHintLevel(1))))
	if msg != ErrHintUnavailable.Error() {
		t.Fatalf("非训练对话应拒绝提示，实际: %q", msg)
	}
	if st.called != 0 || store.hintCount != 0 {
		t.Fatal("非训练对话提示不应调起 AI 或落库")
	}
}

func TestHintRejectedWhenProviderNotWired(t *testing.T) {
	store := newFakeStore()
	st := newStreamerWithDone()
	svc := NewChatService(store, st, "mock", "") // 不 EnableHints
	convID := startTrainingConvWithSession(t, svc, 7, 700)

	msg := onlyError(collect(svc.StreamReply(
		context.Background(), 7, convID, "提示", WithHintLevel(1))))
	if msg != ErrHintUnavailable.Error() {
		t.Fatalf("未装配配额供给应拒绝提示，实际: %q", msg)
	}
	// EnableHints(nil) 应被忽略。
	svc.EnableHints(nil)
	if svc.hintQuota != nil {
		t.Fatal("nil provider 不应被装配")
	}
}

func TestHintLevelOutOfRange(t *testing.T) {
	svc, _, _, _, convID := hintHarness(t, "easy")
	for _, lvl := range []int{-1, 4, 99} {
		msg := onlyError(collect(svc.StreamReply(
			context.Background(), 7, convID, "提示", WithHintLevel(lvl))))
		if msg != ErrHintLevelInvalid.Error() {
			t.Fatalf("级别 %d 应判非法，实际: %q", lvl, msg)
		}
	}
}

// ---- fail-closed：供给读失败一律拒绝，绝不超额泄露 ----

func TestHintFailClosedOnProviderErrors(t *testing.T) {
	t.Run("难度读取失败", func(t *testing.T) {
		svc, store, st, provider, convID := hintHarness(t, "easy")
		provider.difficultyErr = errors.New("pg down")
		msg := onlyError(collect(svc.StreamReply(
			context.Background(), 7, convID, "提示", WithHintLevel(1))))
		if msg == "" || !strings.Contains(msg, "难度") {
			t.Fatalf("难度读取失败应返回错误帧，实际: %q", msg)
		}
		if st.called != 0 || store.upCount != 0 {
			t.Fatal("fail-closed：不应调 AI、不应落用户消息")
		}
	})
	t.Run("用量读取失败", func(t *testing.T) {
		svc, store, st, provider, convID := hintHarness(t, "easy")
		provider.usageErr = errors.New("pg down")
		msg := onlyError(collect(svc.StreamReply(
			context.Background(), 7, convID, "提示", WithHintLevel(1))))
		if msg == "" || !strings.Contains(msg, "用量") {
			t.Fatalf("用量读取失败应返回错误帧，实际: %q", msg)
		}
		if st.called != 0 || store.upCount != 0 {
			t.Fatal("fail-closed：不应调 AI、不应落用户消息")
		}
	})
}

// ---- 提示消息落库与普通应答互不影响 ----

func TestHintPersistedAsHintMessage(t *testing.T) {
	svc, store, _, _, convID := hintHarness(t, "easy")

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "普通追问"))
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "求提示", WithHintLevel(1)))
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "求提示", WithHintLevel(2)))

	if store.asCount != 1 || store.hintCount != 2 {
		t.Fatalf("应答/提示应分别落库: assistant=%d hint=%d", store.asCount, store.hintCount)
	}
	if store.hints[0].level != 1 || store.hints[1].level != 2 {
		t.Fatalf("提示级别落库错误: %+v", store.hints)
	}
	if store.hints[0].sessionID != 700 {
		t.Fatalf("提示消息应能反查会话: %+v", store.hints[0])
	}
}

// 普通应答完全不碰配额供给，行为与未启用提示时一致。
func TestNormalReplyDoesNotTouchQuota(t *testing.T) {
	svc, _, st, provider, convID := hintHarness(t, "easy")

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "普通问题"))
	last := evs[len(evs)-1]
	if last.Type != StreamDone || last.HintLevel != 0 || last.HintRemaining != -1 {
		t.Fatalf("普通应答 done 帧应为 0/-1: %+v", last)
	}
	if st.got.HintLevel != 0 || st.got.HintRemaining != -1 {
		t.Fatalf("普通应答 AI 入参应为 0/-1: %+v", st.got)
	}
	if provider.difficultyHits != 0 || provider.usageHits != 0 {
		t.Fatalf("普通应答不应读取配额供给: diff=%d usage=%d",
			provider.difficultyHits, provider.usageHits)
	}
}

// ---- 提示轮跳过坑点判定与注入 ----

func TestHintTurnSkipsPitfall(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer() // 即便坑点条件命中，提示轮也应走 LLM
	provider := &fakeHintQuotaProvider{store: store, difficulty: "easy"}
	svc := NewChatService(store, st, "mock", "")
	svc.EnableHints(provider)
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{pfConfig(1, 100)},
		snap:    triggeredSnap(),
	}
	svc.EnablePitfall(rt, NewEngine(nil))
	convID := startTrainingConvWithSession(t, svc, 7, 700)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "求提示", WithHintLevel(1)))

	if rt.activeConfigHits != 0 || rt.snapshotHits != 0 {
		t.Fatalf("提示轮不应做坑点判定/快照: configs=%d snap=%d",
			rt.activeConfigHits, rt.snapshotHits)
	}
	if st.called != 1 {
		t.Fatal("提示轮应直接走 LLM，不被坑点模板截胡")
	}
	last := evs[len(evs)-1]
	if last.Type != StreamDone || last.HintLevel != 1 || last.IsPitfall {
		t.Fatalf("提示轮 done 帧异常: %+v", last)
	}
	if store.pfCount != 0 || store.hintCount != 1 {
		t.Fatalf("提示轮不应落坑点消息，应落提示消息: pf=%d hint=%d",
			store.pfCount, store.hintCount)
	}
}

// errSentinel 把错误文案还原为哨兵错误，便于 errors.Is 比对（编排层经 SSE 只传文案）。
func errSentinel(msg string) error {
	switch msg {
	case ErrHintUnavailable.Error():
		return ErrHintUnavailable
	case ErrHintLevelInvalid.Error():
		return ErrHintLevelInvalid
	case ErrHintExhausted.Error():
		return ErrHintExhausted
	}
	if strings.Contains(msg, ErrHintLevelLocked.Error()) {
		return ErrHintLevelLocked
	}
	return errors.New(msg)
}
