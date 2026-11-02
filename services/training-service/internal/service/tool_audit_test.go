package service

import (
	"context"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	oetest "github.com/quenching/proto/operationevidence/testutil"
	"testing"
)

type failingToolStore struct {
	oe.Store
	failBegin, failAppend bool
}

func (f failingToolStore) Begin(ctx context.Context, sid int64, id, key, hash string, e oe.Evidence) (bool, error) {
	if f.failBegin {
		return false, errors.New("db unavailable")
	}
	return f.Store.Begin(ctx, sid, id, key, hash, e)
}
func (f failingToolStore) Append(ctx context.Context, e oe.Evidence, texts []oe.Text) error {
	if f.failAppend {
		return errors.New("save unavailable")
	}
	return f.Store.Append(ctx, e, texts)
}
func TestToolAuditReadonlySuccessAndFailure(t *testing.T) {
	for _, bad := range []bool{false, true} {
		store := oetest.NewMemoryStore()
		audit := &ToolAudit{Store: store}
		ctx := WithChatLease(context.Background(), ChatLeaseScope{UserID: 7, SessionID: 1})
		e, err := audit.Begin(ctx, ChatLeaseScope{UserID: 7, SessionID: 1}, 10, 1, ToolCall{ID: "call", Name: "get_logs", ArgumentsJSON: `{}`})
		if err != nil {
			t.Fatal(err)
		}
		e, err = audit.Advance(ctx, e, "running", "")
		if err != nil {
			t.Fatal(err)
		}
		if err = audit.Finish(ctx, e, ToolResult{ContentJSON: `{"lines":["tail"]}`, IsError: bad}); err != nil {
			t.Fatal(err)
		}
		v, _ := store.Get(ctx, 1, e.OperationID)
		if v.Actor != "assistant" || !oe.Terminal(v.Status) || len(v.TextRefs) != 1 {
			t.Fatal(v)
		}
	}
}
func TestToolAuditApprovalRejectedAndTimedOut(t *testing.T) {
	audit := &ToolAudit{Store: oetest.NewMemoryStore()}
	for _, status := range []string{"rejected", "timed_out"} {
		e, err := audit.Begin(context.Background(), ChatLeaseScope{SessionID: 1}, 10, 0, ToolCall{ID: status, Name: "write_file", ArgumentsJSON: `{}`})
		if err != nil {
			t.Fatal(err)
		}
		e, err = audit.Advance(context.Background(), e, "cancelled", status)
		if err != nil || e.Status != "cancelled" || e.Approval != status {
			t.Fatal(e, err)
		}
	}
}
func TestToolAuditBeginFailureDoesNotExecute(t *testing.T) {
	audit := &ToolAudit{Store: failingToolStore{Store: oetest.NewMemoryStore(), failBegin: true}}
	if _, err := audit.Begin(context.Background(), ChatLeaseScope{SessionID: 1}, 10, 0, ToolCall{ID: "c", Name: "get_logs", ArgumentsJSON: `{}`}); err == nil {
		t.Fatal("execute would start")
	}
}
func TestToolAuditResultFailureNoReplay(t *testing.T) {
	s := oetest.NewMemoryStore()
	audit := &ToolAudit{Store: s}
	e, _ := audit.Begin(context.Background(), ChatLeaseScope{SessionID: 1}, 10, 0, ToolCall{ID: "c", Name: "write_file", ArgumentsJSON: `{}`})
	audit.Store = failingToolStore{Store: s, failAppend: true}
	if audit.Finish(context.Background(), e, ToolResult{ContentJSON: `{}`}) == nil {
		t.Fatal("silent record failure")
	}
	v, _ := s.Get(context.Background(), 1, e.OperationID)
	if v.Status != "pending" {
		t.Fatal(v)
	}
}
func TestHistoryToolAuditDoesNotCopyHistory(t *testing.T) {
	s := oetest.NewMemoryStore()
	audit := &ToolAudit{Store: s}
	e, _ := audit.Begin(context.Background(), ChatLeaseScope{SessionID: 1}, 10, 0, ToolCall{ID: "c", Name: "read_session_history", ArgumentsJSON: `{}`})
	if err := audit.Finish(context.Background(), e, ToolResult{ContentJSON: `{"sources":[{"id":"message:1","text":"history body"}]}`}); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(context.Background(), 1, e.OperationID)
	if len(v.TextRefs) != 0 {
		t.Fatal("history duplicated")
	}
}
