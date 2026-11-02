package service

import (
	"context"
	"encoding/json"
	"github.com/quenching/training-service/internal/model"
	"strings"
	"testing"
)

type historyFake struct{ SessionMemoryStore }

func (*historyFake) Resolve(_ context.Context, u, s int64) (*model.SessionConversation, error) {
	if u != 10 || s != 7 {
		return nil, ErrSessionMemoryNotFound
	}
	return &model.SessionConversation{ConversationID: 1, SessionID: 7, SessionStatus: "running"}, nil
}
func (*historyFake) Search(_ context.Context, u, s int64, q string, b int64, n int) (model.SessionHistoryPage, error) {
	return model.SessionHistoryPage{Messages: []model.SessionMessage{{ID: 1, Role: "user", Content: "database restart failed"}}}, nil
}
func (*historyFake) ReadReferences(_ context.Context, u, s int64, ids []int64, eids []string) (model.MemorySourcePage, error) {
	if len(eids) > 0 {
		return model.MemorySourcePage{}, ErrSessionMemoryNotFound
	}
	return model.MemorySourcePage{Sources: []model.MemorySource{{ID: "message:1", Kind: "message", Role: "user", Text: `password="very secret value" and Bearer abc123`}}}, nil
}
func TestSearchFindsEarlyFailedAttempt(t *testing.T) {
	out, e := NewSessionHistoryTools(&historyFake{}).Execute(context.Background(), 10, 7, ToolCall{Name: "search_session_history", ArgumentsJSON: `{"query":"restart"}`})
	if e != nil || !strings.Contains(out, "message:1") || !strings.Contains(out, "restart failed") {
		t.Fatal(out, e)
	}
}
func TestHistoryToolCannotSwitchSession(t *testing.T) {
	h := NewSessionHistoryTools(&historyFake{})
	if _, e := h.Execute(context.Background(), 10, 7, ToolCall{Name: "search_session_history", ArgumentsJSON: `{"query":"restart","session_id":8}`}); e == nil {
		t.Fatal("model changed session")
	}
	if _, e := h.Execute(context.Background(), 11, 7, ToolCall{Name: "search_session_history", ArgumentsJSON: `{"query":"restart"}`}); e == nil {
		t.Fatal("foreign user read")
	}
}
func TestRetrievedSourcesRedactSecrets(t *testing.T) {
	out, e := NewSessionHistoryTools(&historyFake{}).Execute(context.Background(), 10, 7, ToolCall{Name: "read_session_history", ArgumentsJSON: `{"message_ids":[1]}`})
	if e != nil || strings.Contains(out, "very secret") || strings.Contains(out, "abc123") {
		t.Fatal(out, e)
	}
}
func TestReadToolRejectsPrivateEvidence(t *testing.T) {
	_, e := NewSessionHistoryTools(&historyFake{}).Execute(context.Background(), 10, 7, ToolCall{Name: "read_session_history", ArgumentsJSON: `{"evidence_ids":["private:injection"]}`})
	if e == nil {
		t.Fatal("private evidence allowed")
	}
}

type bigHistoryFake struct{ historyFake }

func (*bigHistoryFake) Search(context.Context, int64, int64, string, int64, int) (model.SessionHistoryPage, error) {
	return model.SessionHistoryPage{Messages: []model.SessionMessage{{ID: 1, Role: "user", Content: strings.Repeat("a", 8000)}, {ID: 2, Role: "user", Content: strings.Repeat("b", 8000)}, {ID: 3, Role: "user", Content: strings.Repeat("c", 8000)}}}, nil
}
func TestHistoryBudgetPreservesPagination(t *testing.T) {
	raw, e := NewSessionHistoryTools(&bigHistoryFake{}).Execute(context.Background(), 10, 7, ToolCall{Name: "search_session_history", ArgumentsJSON: `{"query":"old"}`})
	if e != nil {
		t.Fatal(e)
	}
	var data struct {
		Sources []model.MemorySource `json:"sources"`
		Next    int64                `json:"next_before_id"`
		More    bool                 `json:"has_more"`
	}
	if e = json.Unmarshal([]byte(raw), &data); e != nil {
		t.Fatal(e)
	}
	if len(data.Sources) != 1 || data.Sources[0].ID != "message:3" || data.Next != 3 || !data.More {
		t.Fatal("omitted sources cannot be paged back", data.Next, data.More)
	}
}

func (*historyFake) ReadSourceRange(_ context.Context, u, s int64, ref string, offset, limit int) (model.MemorySource, error) {
	return model.MemorySource{ID: ref, Kind: "message", Role: "user", Text: "later part", Truncated: true, NextOffsetBytes: offset + limit}, nil
}
func TestReadHistoryCanContinueLongSource(t *testing.T) {
	raw, e := NewSessionHistoryTools(&historyFake{}).Execute(context.Background(), 10, 7, ToolCall{Name: "read_session_history", ArgumentsJSON: `{"reference":"message:1","offset_bytes":8192,"limit_bytes":4096}`})
	if e != nil || !strings.Contains(raw, "later part") || !strings.Contains(raw, "12288") {
		t.Fatal(raw, e)
	}
}

type evidenceHistoryFake struct {
	historyFake
	before int64
}

func (f *evidenceHistoryFake) EvidenceHistory(ctx context.Context, u, s, before int64, n int) (model.MemorySourcePage, error) {
	f.before = before
	return model.MemorySourcePage{Sources: []model.MemorySource{{ID: "operation:1", Sequence: 1, Kind: "operation", Role: "user", Text: "early restart failed"}}}, nil
}
func TestCanDiscoverEarlyPublicEvidenceWithoutKnownReference(t *testing.T) {
	f := &evidenceHistoryFake{}
	h := NewSessionHistoryTools(f)
	raw, e := h.Execute(context.Background(), 10, 7, ToolCall{Name: "list_session_evidence", ArgumentsJSON: `{"before_sequence":12,"limit":5}`})
	if e != nil || f.before != 12 || !strings.Contains(raw, "early restart failed") {
		t.Fatal(raw, e, f.before)
	}
	if _, e = h.Execute(context.Background(), 11, 7, ToolCall{Name: "list_session_evidence", ArgumentsJSON: `{}`}); e == nil {
		t.Fatal("foreign evidence list")
	}
	if _, e = h.Execute(context.Background(), 10, 7, ToolCall{Name: "list_session_evidence", ArgumentsJSON: `{"session_id":8}`}); e == nil {
		t.Fatal("identity override")
	}
}
