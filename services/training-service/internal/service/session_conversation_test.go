package service

import (
	"context"
	"github.com/quenching/training-service/internal/model"
	"sync"
	"testing"
	"time"
)

type memoryFake struct {
	SessionMemoryStore
	mu            sync.Mutex
	uid, sid, cid int64
	status        string
	busy          bool
	renewOK       bool
	store         *fakeStore
	page          model.SessionHistoryPage
}

func (f *memoryFake) Resolve(_ context.Context, u, s int64) (*model.SessionConversation, error) {
	if u != f.uid || s != f.sid {
		return nil, ErrSessionMemoryNotFound
	}
	return &model.SessionConversation{ConversationID: f.cid, SessionID: s, SessionStatus: f.status}, nil
}
func (f *memoryFake) History(ctx context.Context, u, s, b int64, n int) (model.SessionHistoryPage, error) {
	_, e := f.Resolve(ctx, u, s)
	return f.page, e
}
func (f *memoryFake) Acquire(_ context.Context, u, s int64, token string, ttl time.Duration) (model.ChatLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy {
		return model.ChatLease{}, ErrChatBusy
	}
	f.busy = true
	return model.ChatLease{Token: token, Fence: 1, ConversationID: f.cid}, nil
}
func (f *memoryFake) Renew(_ context.Context, u, s int64, l model.ChatLease, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewOK, nil
}
func (f *memoryFake) Release(_ context.Context, u, s int64, l model.ChatLease) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.busy = false
	return nil
}
func (f *memoryFake) AppendLeasedMessage(ctx context.Context, u, s int64, l model.ChatLease, m model.SessionMessage) (int64, error) {
	if m.Role == "user" {
		return f.store.AppendUserMessage(ctx, l.ConversationID, m.Content)
	}
	return f.store.AppendAssistantMessage(ctx, l.ConversationID, m.Content, m.Tokens, m.LatencyMS, m.Cost, false, "")
}
func sessionChat(t *testing.T, ai ChatStreamer) (*ChatService, *memoryFake) {
	t.Helper()
	st := newFakeStore()
	sid := int64(7)
	c := &model.Conversation{UserID: 10, SessionID: &sid, ConversationType: model.ConvTrainingAssistant, Status: model.ConvStatusActive}
	id, _ := st.CreateConversation(context.Background(), c)
	f := &memoryFake{uid: 10, sid: 7, cid: id, status: "running", renewOK: true, store: st, page: model.SessionHistoryPage{Messages: []model.SessionMessage{{ID: 1, Role: "user", Content: "older"}}}}
	svc := NewChatService(st, ai, "qwen3.8-flash", "")
	svc.WithSessionMemory(f)
	return svc, f
}
func TestRestoreSessionConversationOwnership(t *testing.T) {
	s, _ := sessionChat(t, nil)
	if _, e := s.RestoreSessionConversation(context.Background(), 11, 7); e != ErrSessionMemoryNotFound {
		t.Fatal(e)
	}
	p, e := s.RestoreSessionConversation(context.Background(), 10, 7)
	if e != nil || len(p.Messages) != 1 || p.ConversationID == 0 {
		t.Fatal(p, e)
	}
}
func TestTrainingCreateReusesCanonicalConversation(t *testing.T) {
	s, f := sessionChat(t, nil)
	sid := int64(7)
	a, e := s.StartConversation(context.Background(), 10, &sid, model.ConvTrainingAssistant)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.StartConversation(context.Background(), 10, &sid, model.ConvTrainingAssistant)
	if e != nil || a != b || a != f.cid || len(f.store.convs) != 1 {
		t.Fatal(a, b, e)
	}
}
func TestClosedSessionHistoryReadOnly(t *testing.T) {
	s, f := sessionChat(t, nil)
	f.status = "completed"
	p, e := s.RestoreSessionConversation(context.Background(), 10, 7)
	if e != nil || p.SessionStatus != "completed" {
		t.Fatal(p, e)
	}
	events := s.StreamReply(context.Background(), 10, f.cid, "new")
	var failure string
	for e := range events {
		if e.Type == StreamError {
			failure = e.ErrorMsg
		}
	}
	if failure == "" || f.store.upCount != 0 {
		t.Fatal("closed session wrote user message", failure)
	}
}

type waitingChat struct{ entered chan struct{} }

func (w *waitingChat) StreamChat(ctx context.Context, in ChatStreamInput) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent)
	select {
	case w.entered <- struct{}{}:
	default:
	}
	go func() { defer close(ch); <-ctx.Done() }()
	return ch, nil
}
func TestConcurrentChatRejected(t *testing.T) {
	w := &waitingChat{entered: make(chan struct{}, 1)}
	s, f := sessionChat(t, w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := s.StreamReply(ctx, 10, f.cid, "one")
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("not entered")
	}
	second := s.StreamReply(context.Background(), 10, f.cid, "two")
	found := false
	for e := range second {
		if e.Type == StreamError && e.ErrorMsg == ErrChatBusy.Error() {
			found = true
		}
	}
	cancel()
	for range first {
	}
	if !found || f.store.upCount != 1 {
		t.Fatal("concurrent request not rejected")
	}
}
func TestLeaseLossCancelsReply(t *testing.T) {
	w := &waitingChat{entered: make(chan struct{}, 1)}
	s, f := sessionChat(t, w)
	f.renewOK = false
	s.chatRenewInterval = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range s.StreamReply(ctx, 10, f.cid, "one") {
	}
	if f.store.asCount != 0 {
		t.Fatal("reply written after lost lease")
	}
}
