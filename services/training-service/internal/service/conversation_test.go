package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// ---- 内存 store：编排层单测无需 PG ----

type fakeStore struct {
	convs     map[int64]*model.Conversation
	msgs      map[int64][]model.HistoryTurn
	nextID    int64
	upCount   int
	asCount   int
	pfCount   int
	hintCount int           // T3-5 落库的提示消息数
	hints     []fakeHint    // T3-5 落库的提示消息（含级别，供断言）
	pitfalls  []fakePitfall // 落库的坑点消息
	failOn    string        // 让某步骤返回错误

	// T3-11 反应判定计数（镜像 training_sessions 的两列）。
	recognizedCount int
	fallenCount     int
}

// fakeHint 记录一条落库的提示型助手消息（T3-5），供配额/级别断言。
type fakeHint struct {
	convID    int64
	sessionID int64
	level     int
	content   string
}

// fakePitfall 记录一条落库的坑点助手消息，供编排测试断言。
type fakePitfall struct {
	messageID       int64
	convID          int64
	content         string
	configID        int64
	pitfallType     string
	confidenceLevel string
	injectedAt      time.Time
	responseType    string // 空=尚未判定（pending）
}

func newFakeStore() *fakeStore {
	return &fakeStore{convs: map[int64]*model.Conversation{}, msgs: map[int64][]model.HistoryTurn{}, nextID: 1}
}

func (f *fakeStore) CreateConversation(_ context.Context, c *model.Conversation) (int64, error) {
	id := f.nextID
	f.nextID++
	c.ID = id
	cp := *c
	f.convs[id] = &cp
	f.msgs[id] = nil
	return id, nil
}

func (f *fakeStore) GetConversation(_ context.Context, id, userID int64) (*model.Conversation, error) {
	c, ok := f.convs[id]
	if !ok || c.UserID != userID {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (f *fakeStore) ListHistory(_ context.Context, convID int64, _ int) ([]model.HistoryTurn, error) {
	return append([]model.HistoryTurn(nil), f.msgs[convID]...), nil
}

func (f *fakeStore) AppendUserMessage(_ context.Context, convID int64, content string) (int64, error) {
	f.upCount++
	f.msgs[convID] = append(f.msgs[convID], model.HistoryTurn{Role: model.RoleUser, Content: content})
	if c := f.convs[convID]; c != nil {
		c.MessageCount++
	}
	return int64(len(f.msgs[convID])), nil
}

func (f *fakeStore) AppendAssistantMessage(_ context.Context, convID int64, content string, _, _ int, _ float64, _ bool, _ string) (int64, error) {
	f.asCount++
	f.msgs[convID] = append(f.msgs[convID], model.HistoryTurn{Role: model.RoleAssistant, Content: content})
	if c := f.convs[convID]; c != nil {
		c.MessageCount++
	}
	return int64(len(f.msgs[convID])), nil
}

func (f *fakeStore) AppendHintMessage(
	_ context.Context, convID int64, content string, hintLevel int, _, _ int, _ float64,
) (int64, error) {
	f.hintCount++
	var sessionID int64
	if c := f.convs[convID]; c != nil && c.SessionID != nil {
		sessionID = *c.SessionID
		c.MessageCount++
	}
	f.hints = append(f.hints, fakeHint{
		convID: convID, sessionID: sessionID, level: hintLevel, content: content,
	})
	f.msgs[convID] = append(f.msgs[convID], model.HistoryTurn{Role: model.RoleAssistant, Content: content})
	return int64(len(f.msgs[convID])), nil
}

// hintUsage 仿线上仓储口径：按会话跨对话统计提示次数与已用最高级别。
func (f *fakeStore) hintUsage(sessionID int64) HintUsage {
	u := HintUsage{}
	for _, h := range f.hints {
		if h.sessionID != sessionID {
			continue
		}
		u.Used++
		if h.level > u.MaxLevel {
			u.MaxLevel = h.level
		}
	}
	return u
}

func (f *fakeStore) AppendPitfallMessage(
	_ context.Context, convID int64, content string, configID int64, pitfallType, confidenceLevel string,
) (int64, error) {
	f.pfCount++
	id := f.nextID
	f.nextID++
	f.pitfalls = append(f.pitfalls, fakePitfall{
		messageID: id, convID: convID, content: content, configID: configID,
		pitfallType: pitfallType, confidenceLevel: confidenceLevel, injectedAt: time.Now(),
	})
	f.msgs[convID] = append(f.msgs[convID], model.HistoryTurn{Role: model.RoleAssistant, Content: content})
	if c := f.convs[convID]; c != nil {
		c.MessageCount++
	}
	return id, nil
}

// seedPendingPitfall 直接播种一条"已注入、未判定"的坑点消息，
// 供 T3-11 反应判定编排测试构造"注入轮之后的反应轮"场景。
func (f *fakeStore) seedPendingPitfall(convID int64, configID int64, injectedAt time.Time) fakePitfall {
	id := f.nextID
	f.nextID++
	pf := fakePitfall{
		messageID:  id,
		convID:     convID,
		content:    "直接重启服务就能解决，不用看日志",
		configID:   configID,
		injectedAt: injectedAt,
	}
	f.pitfalls = append(f.pitfalls, pf)
	return pf
}

// ListPendingPitfalls 返回本对话尚未判定的坑点消息（保持注入先后顺序）。
func (f *fakeStore) ListPendingPitfalls(_ context.Context, convID int64) ([]PendingPitfall, error) {
	if f.failOn == "list_pending" {
		return nil, errors.New("pending store down")
	}
	out := make([]PendingPitfall, 0)
	for _, p := range f.pitfalls {
		if p.convID == convID && p.responseType == "" {
			out = append(out, PendingPitfall{
				MessageID: p.messageID, ConfigID: p.configID, InjectedAt: p.injectedAt,
			})
		}
	}
	return out, nil
}

// MarkPitfallResponse 幂等回写：仅首次写入生效并维护识别/踩坑计数。
func (f *fakeStore) MarkPitfallResponse(
	_ context.Context, convID, messageID int64, responseType string,
) (bool, error) {
	if f.failOn == "mark_response" {
		return false, errors.New("mark store down")
	}
	for i := range f.pitfalls {
		p := &f.pitfalls[i]
		if p.messageID != messageID || p.convID != convID {
			continue
		}
		if p.responseType != "" {
			return false, nil // 已被并发抢先判定
		}
		p.responseType = responseType
		switch responseType {
		case "agree":
			f.fallenCount++
		case "dispute", "verified", "ignore":
			f.recognizedCount++
		}
		return true, nil
	}
	return false, nil
}

// ---- 可控 streamer：按预设序列产出事件，并记录收到的入参 ----

type fakeStreamer struct {
	events []StreamEvent
	got    ChatStreamInput
	called int
}

func (s *fakeStreamer) StreamChat(_ context.Context, in ChatStreamInput) (<-chan StreamEvent, error) {
	s.called++
	s.got = in
	ch := make(chan StreamEvent, len(s.events))
	for _, e := range s.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func collect(ch <-chan StreamEvent) []StreamEvent {
	var out []StreamEvent
	for e := range ch {
		out = append(out, e)
	}
	return out
}

func TestStartConversation(t *testing.T) {
	svc := NewChatService(newFakeStore(), &fakeStreamer{}, "mock", "")
	id, err := svc.StartConversation(context.Background(), 7, nil, model.ConvTrainingAssistant)
	if err != nil || id != 1 {
		t.Fatalf("create conv: id=%d err=%v", id, err)
	}
	if _, err := svc.StartConversation(context.Background(), 7, nil, "bad_type"); err == nil {
		t.Fatal("非法对话类型应当报错")
	}
}

func TestStreamReplyHappyPath(t *testing.T) {
	store := newFakeStore()
	st := &fakeStreamer{events: []StreamEvent{
		{Type: StreamThinking},
		{Type: StreamDelta, Delta: "你"},
		{Type: StreamDelta, Delta: "好"},
		{Type: StreamDone},
	}}
	svc := NewChatService(store, st, "mock", "")
	convID, _ := svc.StartConversation(context.Background(), 7, nil, model.ConvTrainingAssistant)

	events := collect(svc.StreamReply(context.Background(), 7, convID, "在吗"))

	if st.got.Message != "在吗" || st.got.ConversationType != model.ConvTrainingAssistant {
		t.Fatalf("传给 AI 的入参错误: %+v", st.got)
	}
	if st.got.PitfallInstruction {
		t.Fatal("MVP 训练对话不应带坑点指令")
	}
	// 事件序列：thinking, delta, delta, done
	if len(events) != 4 ||
		events[0].Type != StreamThinking ||
		events[1].Type != StreamDelta || events[1].Delta != "你" ||
		events[2].Type != StreamDelta {
		t.Fatalf("事件序列异常: %+v", events)
	}
	done := events[len(events)-1]
	if done.Type != StreamDone || done.MessageID == 0 || done.ConversationID != convID {
		t.Fatalf("结束帧异常: %+v", done)
	}
	// 用户消息与助手回复各落库一次
	if store.upCount != 1 || store.asCount != 1 {
		t.Fatalf("落库次数 user=%d assistant=%d", store.upCount, store.asCount)
	}
	// 首轮：历史为空（本次消息走 message，不应重复进 history）
	if len(st.got.History) != 0 {
		t.Fatalf("首轮历史应为空，实际: %+v", st.got.History)
	}
}

func TestStreamReplyCarriesPriorTurns(t *testing.T) {
	store := newFakeStore()
	st := &fakeStreamer{events: []StreamEvent{{Type: StreamDelta, Delta: "答"}, {Type: StreamDone}}}
	svc := NewChatService(store, st, "mock", "")
	convID, _ := svc.StartConversation(context.Background(), 7, nil, model.ConvTrainingAssistant)

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "第一问"))
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "第二问"))

	// 第二轮发给 AI 的历史应含第一轮的 user+assistant，但不含“第二问”本身。
	h := st.got.History
	if len(h) != 2 || h[0].Role != model.RoleUser || h[0].Content != "第一问" ||
		h[1].Role != model.RoleAssistant || h[1].Content != "答" {
		t.Fatalf("多轮历史重建错误: %+v", h)
	}
	if st.got.Message != "第二问" {
		t.Fatalf("本轮消息应为第二问，实际: %s", st.got.Message)
	}
}

func TestStreamReplyOwnershipAndState(t *testing.T) {
	store := newFakeStore()
	svc := NewChatService(store, &fakeStreamer{}, "mock", "")
	convID, _ := svc.StartConversation(context.Background(), 7, nil, model.ConvTrainingAssistant)

	// 其他用户访问 -> not found
	evs := collect(svc.StreamReply(context.Background(), 999, convID, "hi"))
	if len(evs) != 1 || evs[0].Type != StreamError {
		t.Fatalf("越权访问应返回单个错误帧: %+v", evs)
	}

	// 关闭对话后再发 -> closed
	store.convs[convID].Status = model.ConvStatusClosed
	evs = collect(svc.StreamReply(context.Background(), 7, convID, "hi"))
	if len(evs) != 1 || evs[0].ErrorMsg != ErrConversationClosed.Error() {
		t.Fatalf("关闭态应报错: %+v", evs)
	}
}

func TestStreamReplyEmptyMessage(t *testing.T) {
	svc := NewChatService(newFakeStore(), &fakeStreamer{}, "mock", "")
	evs := collect(svc.StreamReply(context.Background(), 7, 1, "   "))
	if len(evs) != 1 || evs[0].Type != StreamError {
		t.Fatalf("空消息应返回错误帧: %+v", evs)
	}
}

func TestStreamReplyAIErrorStillPersisted(t *testing.T) {
	store := newFakeStore()
	st := &fakeStreamer{events: []StreamEvent{
		{Type: StreamDelta, Delta: "片段"},
		{Type: StreamError, ErrorMsg: "模型超时"},
	}}
	svc := NewChatService(store, st, "mock", "")
	convID, _ := svc.StartConversation(context.Background(), 7, nil, model.ConvTrainingAssistant)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "问"))
	last := evs[len(evs)-1]
	if last.Type != StreamError || last.ErrorMsg != "模型超时" {
		t.Fatalf("应以错误帧结束: %+v", evs)
	}
	// 已生成片段仍应落库，不浪费
	if store.asCount != 1 || store.msgs[convID][1].Content != "片段" {
		t.Fatalf("部分输出未保留: as=%d msgs=%+v", store.asCount, store.msgs[convID])
	}
}
