package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// ---- T3-4 故障上下文供给 fake：可控三态（有数据 / 无故障 / 报错）+ 调用记录 ----

type fakeFaultContextProvider struct {
	fc      *FaultContext
	err     error
	calls   int
	gotSess int64
}

func (p *fakeFaultContextProvider) LoadFaultContext(_ context.Context, sessionID int64) (*FaultContext, error) {
	p.calls++
	p.gotSess = sessionID
	return p.fc, p.err
}

func newStreamerWithDone() *fakeStreamer {
	return &fakeStreamer{events: []StreamEvent{
		{Type: StreamDelta, Delta: "先看监控"},
		{Type: StreamDone},
	}}
}

// startTrainingConvWithSession 创建一个绑定指定训练会话的对话。
func startTrainingConvWithSession(t *testing.T, svc *ChatService, userID, sessionID int64) int64 {
	t.Helper()
	sess := sessionID
	convID, err := svc.StartConversation(context.Background(), userID, &sess, model.ConvTrainingAssistant)
	if err != nil {
		t.Fatalf("创建训练对话失败: %v", err)
	}
	return convID
}

func TestFaultContextPassedToAI(t *testing.T) {
	store := newFakeStore()
	st := newStreamerWithDone()
	provider := &fakeFaultContextProvider{fc: &FaultContext{
		SessionID:  42,
		Difficulty: "hard",
		Faults: []FaultContextItem{
			{
				Name:        "订单接口偶发超时",
				Category:    "network",
				Difficulty:  "hard",
				Description: "高峰期 P99 升到 3s",
				Status:      "triggered",
			},
		},
	}}
	svc := NewChatService(store, st, "mock", "")
	svc.EnableFaultContext(provider)
	convID := startTrainingConvWithSession(t, svc, 7, 42)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "从哪查起？"))

	if provider.calls != 1 || provider.gotSess != 42 {
		t.Fatalf("供给应被以 session=42 调用一次，实际 calls=%d sess=%d", provider.calls, provider.gotSess)
	}
	if st.called != 1 || st.got.FaultContextJSON == "" {
		t.Fatalf("AI 入参应携带故障上下文，实际: %+v", st.got)
	}

	// 白名单字段必须在 JSON 中。
	raw := st.got.FaultContextJSON
	for _, want := range []string{"订单接口偶发超时", "network", "hard", "高峰期 P99 升到 3s", "triggered"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("故障上下文缺少白名单字段 %q: %s", want, raw)
		}
	}
	// 结构化断言：反序列化后只有白名单键，不含任何答案列。
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("故障上下文不是合法 JSON: %v", err)
	}
	faults, ok := parsed["faults"].([]any)
	if !ok || len(faults) != 1 {
		t.Fatalf("faults 结构异常: %+v", parsed)
	}
	item, _ := faults[0].(map[string]any)
	for _, banned := range []string{
		"root_cause", "fix_config", "trigger_config",
		"manifestation_config", "amplifier_config",
	} {
		if _, leaked := item[banned]; leaked {
			t.Fatalf("答案列 %s 严禁进入故障上下文: %s", banned, raw)
		}
	}

	// 对话仍正常完成。
	last := evs[len(evs)-1]
	if last.Type != StreamDone {
		t.Fatalf("应以 done 结束: %+v", evs)
	}
}

func TestFaultContextNotLoadedWithoutProvider(t *testing.T) {
	store := newFakeStore()
	st := newStreamerWithDone()
	svc := NewChatService(store, st, "mock", "") // 不调用 EnableFaultContext
	convID := startTrainingConvWithSession(t, svc, 7, 42)

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "hi"))

	if st.got.FaultContextJSON != "" {
		t.Fatalf("未装配供给时上下文应为空，实际: %q", st.got.FaultContextJSON)
	}
}

func TestFaultContextSkippedForNonTrainingConversation(t *testing.T) {
	store := newFakeStore()
	st := newStreamerWithDone()
	provider := &fakeFaultContextProvider{fc: &FaultContext{SessionID: 1, Faults: []FaultContextItem{{Name: "X"}}}}
	svc := NewChatService(store, st, "mock", "")
	svc.EnableFaultContext(provider)
	// 非训练对话：SessionID 为 nil。
	convID, err := svc.StartConversation(context.Background(), 7, nil, model.ConvKnowledgeQA)
	if err != nil {
		t.Fatalf("创建非训练对话失败: %v", err)
	}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "hi"))

	if provider.calls != 0 {
		t.Fatalf("非训练对话不应加载故障上下文，calls=%d", provider.calls)
	}
	if st.got.FaultContextJSON != "" {
		t.Fatalf("非训练对话上下文应为空，实际: %q", st.got.FaultContextJSON)
	}
}

func TestFaultContextProviderErrorDoesNotBlockReply(t *testing.T) {
	store := newFakeStore()
	st := newStreamerWithDone()
	provider := &fakeFaultContextProvider{err: errors.New("pg down")}
	svc := NewChatService(store, st, "mock", "")
	svc.EnableFaultContext(provider)
	convID := startTrainingConvWithSession(t, svc, 7, 42)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "hi"))

	// 加载失败：尽力而为，不带上下文但对话照常完成。
	if st.got.FaultContextJSON != "" {
		t.Fatalf("供给报错时上下文应为空，实际: %q", st.got.FaultContextJSON)
	}
	last := evs[len(evs)-1]
	if last.Type != StreamDone {
		t.Fatalf("供给报错不应阻断对话: %+v", evs)
	}
}

func TestFaultContextNilResultMeansNoContext(t *testing.T) {
	store := newFakeStore()
	st := newStreamerWithDone()
	provider := &fakeFaultContextProvider{fc: nil} // 故障尚未暴露
	svc := NewChatService(store, st, "mock", "")
	svc.EnableFaultContext(provider)
	convID := startTrainingConvWithSession(t, svc, 7, 42)

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "hi"))

	if st.got.FaultContextJSON != "" {
		t.Fatalf("故障未暴露时上下文应为空，实际: %q", st.got.FaultContextJSON)
	}
}

func TestEnableFaultContextNilProviderIgnored(t *testing.T) {
	svc := NewChatService(newFakeStore(), newStreamerWithDone(), "mock", "")
	svc.EnableFaultContext(nil) // 不应 panic，字段保持 nil
	if svc.faultContext != nil {
		t.Fatal("nil provider 不应被装配")
	}
}

// TestHintFieldsForwardedEndToEnd 普通应答（未请求提示）链路：thinking/delta 帧
// 透传 AI 回传字段，done 帧以编排层本地校验结果（0/-1）为准。提示轮的端到端
// 级别/剩余次数断言见 conversation_hint_test.go。
func TestHintFieldsForwardedEndToEnd(t *testing.T) {
	store := newFakeStore()
	st := &fakeStreamer{events: []StreamEvent{
		{Type: StreamThinking, HintLevel: 0, HintRemaining: -1},
		{Type: StreamDelta, Delta: "先看网络", HintLevel: 0, HintRemaining: -1},
		{Type: StreamDone, HintLevel: 0, HintRemaining: -1},
	}}
	svc := NewChatService(store, st, "mock", "")
	convID, _ := svc.StartConversation(context.Background(), 7, nil, model.ConvTrainingAssistant)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "提示一下"))

	var sawDone bool
	for _, ev := range evs {
		if ev.Type == StreamDone {
			if ev.HintLevel != 0 || ev.HintRemaining != -1 {
				t.Fatalf("普通应答 done 帧应为 0/-1: %+v", ev)
			}
			sawDone = true
		}
		if ev.Type == StreamDelta && (ev.HintLevel != 0 || ev.HintRemaining != -1) {
			t.Fatalf("delta 帧应透传提示字段: %+v", ev)
		}
	}
	if !sawDone {
		t.Fatalf("未收到 done 帧: %+v", evs)
	}
}
