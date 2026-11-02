package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/training-service/internal/model"
	"strings"
	"testing"
)

type cumulativeFake struct {
	*memoryFake
	sources   []model.MemorySource
	memory    model.SessionMemory
	saveCalls int
	saveOK    bool
}

func (f *cumulativeFake) Load(context.Context, int64, int64) (*model.SessionMemory, error) {
	m := f.memory
	return &m, nil
}
func (f *cumulativeFake) Sources(_ context.Context, u, s, afterMsg, afterEvidence int64, n int) (model.MemorySourcePage, error) {
	p := model.MemorySourcePage{Sources: []model.MemorySource{}, MessageWatermark: afterMsg, EvidenceWatermark: afterEvidence}
	for _, x := range f.sources {
		if x.Sequence > afterMsg {
			p.Sources = append(p.Sources, x)
			p.MessageWatermark = x.Sequence
		}
	}
	return p, nil
}
func (f *cumulativeFake) ReadReferences(_ context.Context, u, s int64, ids []int64, eids []string) (model.MemorySourcePage, error) {
	p := model.MemorySourcePage{}
	for _, id := range ids {
		for _, x := range f.sources {
			if x.ID == fmt.Sprintf("message:%d", id) {
				p.Sources = append(p.Sources, x)
			}
		}
	}
	if len(p.Sources) != len(ids)+len(eids) {
		return p, ErrSessionMemoryNotFound
	}
	return p, nil
}
func (f *cumulativeFake) SaveCAS(_ context.Context, u, s, v int64, l model.ChatLease, n model.SessionMemory) (bool, error) {
	f.saveCalls++
	if !f.saveOK {
		return false, nil
	}
	n.Version = v + 1
	f.memory = n
	return true, nil
}

type memorySummaryFake struct{ err error }

func (f memorySummaryFake) Summarize(_ context.Context, in model.MemorySummaryInput) (model.MemorySummaryResult, error) {
	if f.err != nil {
		return model.MemorySummaryResult{}, f.err
	}
	var entries []map[string]any
	for _, x := range in.Sources {
		if x.ID == "message:1" {
			entries = append(entries, map[string]any{"kind": "user_statement", "text": x.Text, "refs": []string{x.ID}})
		}
	}
	raw, _ := json.Marshal(map[string]any{"entries": entries})
	return model.MemorySummaryResult{MemoryJSON: string(raw), ActualModel: "qwen3.8-flash"}, nil
}
func memoryFixture(t *testing.T) (*cumulativeFake, context.Context) {
	_, base := sessionChat(t, nil)
	f := &cumulativeFake{memoryFake: base, saveOK: true, memory: model.SessionMemory{SchemaVersion: 1, MemoryJSON: `{"entries":[]}`}}
	for i := 1; i <= 65; i++ {
		text := fmt.Sprintf("message %d", i)
		if i == 1 {
			text = "database restart already tried, still failing"
		}
		f.sources = append(f.sources, model.MemorySource{ID: fmt.Sprintf("message:%d", i), Sequence: int64(i), Kind: "message", Role: "user", Text: text})
		if i > 45 {
			f.page.Messages = append(f.page.Messages, model.SessionMessage{ID: int64(i), Role: "user", Content: text})
		}
	}
	f.page.Messages = f.page.Messages[1:]
	ctx := WithChatLease(context.Background(), ChatLeaseScope{UserID: 10, SessionID: 7, Lease: model.ChatLease{ConversationID: base.cid, Token: "t", Fence: 1}})
	return f, ctx
}
func TestEarlyFactsRemainAfterSixtyMessages(t *testing.T) {
	f, ctx := memoryFixture(t)
	out, e := NewSessionMemoryService(f, memorySummaryFake{}).Prepare(ctx, 10, 7, "what now")
	if e != nil || !strings.Contains(out.MemoryJSON, "restart already tried") || f.memory.MessageWatermark != 45 {
		t.Fatal(out, e, f.memory)
	}
}
func TestSummaryFailureKeepsWatermarkAndRawSources(t *testing.T) {
	f, ctx := memoryFixture(t)
	out, e := NewSessionMemoryService(f, memorySummaryFake{err: errors.New("timeout")}).Prepare(ctx, 10, 7, "what now")
	if e != nil || !out.Degraded || f.memory.MessageWatermark != 0 || f.saveCalls != 0 || !strings.Contains(out.MemoryJSON, "message:1") {
		t.Fatal(out, e)
	}
}
func TestStaleSummaryDoesNotOverwrite(t *testing.T) {
	f, ctx := memoryFixture(t)
	f.saveOK = false
	out, e := NewSessionMemoryService(f, memorySummaryFake{}).Prepare(ctx, 10, 7, "what now")
	if e != nil || !out.Degraded || f.memory.Version != 0 {
		t.Fatal(out, e)
	}
}
func TestContextBudgetHasExplicitCoverage(t *testing.T) {
	f, ctx := memoryFixture(t)
	for i := range f.sources {
		f.sources[i].Text = strings.Repeat("a", 10000)
	}
	for i := range f.page.Messages {
		f.page.Messages[i].Content = strings.Repeat("b", 10000)
	}
	out, e := NewSessionMemoryService(f, memorySummaryFake{err: errors.New("timeout")}).Prepare(ctx, 10, 7, "now")
	if e != nil || len(out.MemoryJSON) > 80*1024 || !out.Degraded || !strings.Contains(out.MemoryJSON, "truncated") {
		t.Fatal(len(out.MemoryJSON), out.Degraded, e)
	}
}

func TestRecentBudgetKeepsNewestTurns(t *testing.T) {
	f, ctx := memoryFixture(t)
	for i := range f.page.Messages {
		f.page.Messages[i].Content = strings.Repeat("x", 8000)
	}
	out, e := NewSessionMemoryService(f, memorySummaryFake{}).Prepare(ctx, 10, 7, "continue")
	if e != nil {
		t.Fatal(e)
	}
	if len(out.History) == 0 || !strings.Contains(out.History[len(out.History)-1].Content, "message:65") {
		t.Fatal("newest turn omitted")
	}
}
func TestTruncatedSourceNeverClaimsFullCoverage(t *testing.T) {
	f, ctx := memoryFixture(t)
	f.sources[0].Text = strings.Repeat("x", 9000) + "important tail"
	out, e := NewSessionMemoryService(f, briefMemorySummary{}).Prepare(ctx, 10, 7, "continue")
	if e != nil {
		t.Fatal(e)
	}
	if f.memory.MessageWatermark != 0 || !out.Degraded {
		t.Fatal("unread tail marked covered", f.memory.MessageWatermark, out.Degraded)
	}
}
func TestStudentReportCannotBecomeObservation(t *testing.T) {
	var src model.MemorySource
	_ = json.Unmarshal([]byte(`{"id":"report:1","kind":"operation","actor":"student_report","role":"user","successful":true}`), &src)
	for _, kind := range []string{"observation", "verified"} {
		raw := fmt.Sprintf(`{"entries":[{"kind":%q,"text":"restart worked","refs":["report:1"]}]}`, kind)
		if _, e := validateMemory(raw, []model.MemorySource{src}); e == nil {
			t.Fatal("self report promoted", kind)
		}
	}
	if _, e := validateMemory(`{"entries":[{"kind":"user_statement","text":"said restart worked","refs":["report:1"]}]}`, []model.MemorySource{src}); e != nil {
		t.Fatal(e)
	}
}

type briefMemorySummary struct{}

func (briefMemorySummary) Summarize(context.Context, model.MemorySummaryInput) (model.MemorySummaryResult, error) {
	return model.MemorySummaryResult{MemoryJSON: `{"entries":[{"kind":"user_statement","text":"brief prefix","refs":["message:1"]}]}`, ActualModel: "qwen3.8-flash"}, nil
}
