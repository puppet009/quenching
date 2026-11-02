package service

import (
	"context"
	"encoding/json"
	"github.com/quenching/training-service/internal/model"
	"strings"
	"testing"
	"time"
)

func TestSessionQuotedSecretsAndAuthorization(t *testing.T) {
	for _, text := range []string{`example {"password": "quoted sensitive value"}`, `Authorization: Basic encodedSensitiveValue`, `jwt="signedSensitiveValue"`} {
		out := RedactSessionText(text)
		if strings.Contains(out, "sensitive") || strings.Contains(out, "Sensitive") {
			t.Fatal("secret persisted in session history", out)
		}
	}
}
func TestMemoryRequestRejectsMissingInternalToken(t *testing.T) {
	c := &AIClient{}
	if _, e := c.Summarize(context.Background(), model.MemorySummaryInput{SessionID: 7}); e == nil {
		t.Fatal("missing token accepted")
	}
}

type memoryAgentFixture struct {
	calls           int
	seenMemory      bool
	seenHistoryTool bool
}

func (a *memoryAgentFixture) StreamChat(_ context.Context, in ChatStreamInput) (<-chan StreamEvent, error) {
	a.calls++
	a.seenMemory = a.seenMemory || strings.Contains(in.MemoryContextJSON, "restart already tried")
	for _, d := range in.Tools {
		a.seenHistoryTool = a.seenHistoryTool || d.Name == "search_session_history"
	}
	ch := make(chan StreamEvent, 2)
	if len(in.ToolResults) == 0 {
		ch <- StreamEvent{Type: StreamToolCall, ToolCalls: []ToolCall{{ID: "lookup", Name: "search_session_history", ArgumentsJSON: `{"query":"restart"}`}}}
	} else {
		ch <- StreamEvent{Type: StreamDelta, Delta: "prior restart was ineffective"}
		ch <- StreamEvent{Type: StreamDone}
	}
	close(ch)
	return ch, nil
}
func TestMemoryAssistantWorksWithoutSandboxTools(t *testing.T) {
	f, _ := memoryFixture(t)
	a := &memoryAgentFixture{}
	svc := NewChatService(f.store, a, "qwen3.8-flash", "")
	svc.WithSessionMemory(f)
	svc.EnableSessionMemoryContext(NewSessionMemoryService(f, memorySummaryFake{}))
	svc.EnableSessionHistoryTools(NewSessionHistoryTools(&historyFake{}))
	var reply string
	for e := range svc.StreamReply(context.Background(), 10, f.cid, "what was tried?") {
		if e.Type == StreamError {
			t.Fatal(e.ErrorMsg)
		}
		if e.Type == StreamDelta {
			reply += e.Delta
		}
	}
	if a.calls != 2 || !a.seenMemory || !a.seenHistoryTool || reply != "prior restart was ineffective" || f.store.asCount != 1 {
		t.Fatal(a, reply, f.store.asCount)
	}
}

func TestNestedSessionSecretsRemainValidJSON(t *testing.T) {
	for _, raw := range []string{`{"data":"example {\"password\": \"quoted sensitive value\"}"}`, `{"items":["Authorization: Basic encodedSensitiveValue",{"data":"jwt=\"signedSensitiveValue\""}]}`} {
		out := RedactSessionText(raw)
		if !json.Valid([]byte(out)) || strings.Contains(strings.ToLower(out), "sensitive") {
			t.Fatal(out)
		}
	}
}

type inputCaptureChat struct{ input ChatStreamInput }

func (a *inputCaptureChat) StreamChat(_ context.Context, in ChatStreamInput) (<-chan StreamEvent, error) {
	a.input = in
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Type: StreamDelta, Delta: "safe"}
	ch <- StreamEvent{Type: StreamDone}
	close(ch)
	return ch, nil
}
func TestCurrentSessionInputIsRedactedBeforeModel(t *testing.T) {
	a := &inputCaptureChat{}
	s, f := sessionChat(t, a)
	for e := range s.StreamReply(context.Background(), 10, f.cid, `password="sensitive value" Authorization: Basic encodedSensitiveValue`) {
		if e.Type == StreamError {
			t.Fatal(e.ErrorMsg)
		}
	}
	if strings.Contains(strings.ToLower(a.input.Message), "sensitive") {
		t.Fatal(a.input.Message)
	}
}

type deadlineMemoryFake struct{ *memoryFake }

func (f *deadlineMemoryFake) AppendLeasedMessage(ctx context.Context, u, s int64, l model.ChatLease, m model.SessionMessage) (int64, error) {
	if e := ctx.Err(); e != nil {
		return 0, e
	}
	return f.memoryFake.AppendLeasedMessage(ctx, u, s, l, m)
}

type slowMemoryChat struct{}

func (slowMemoryChat) StreamChat(ctx context.Context, in ChatStreamInput) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 2)
	go func() {
		defer close(ch)
		select {
		case <-time.After(5100 * time.Millisecond):
			ch <- StreamEvent{Type: StreamDelta, Delta: "slow answer"}
			ch <- StreamEvent{Type: StreamDone}
		case <-ctx.Done():
		}
	}()
	return ch, nil
}
func TestSlowSessionReplyPersistsAfterFiveSeconds(t *testing.T) {
	s, f := sessionChat(t, slowMemoryChat{})
	s.WithSessionMemory(&deadlineMemoryFake{f})
	for e := range s.StreamReply(context.Background(), 10, f.cid, "wait") {
		if e.Type == StreamError {
			t.Fatal(e.ErrorMsg)
		}
	}
	if f.store.asCount != 1 {
		t.Fatal("reply not persisted")
	}
}
