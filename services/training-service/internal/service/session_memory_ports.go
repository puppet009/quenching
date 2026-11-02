package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/training-service/internal/model"
	"regexp"
	"time"
)

var ErrSessionMemoryNotFound = errors.New("班次不存在或无权访问")
var ErrChatBusy = errors.New("实训助手正在回复，请稍后再试")
var ErrChatLeaseLost = errors.New("对话租约已失效，请重新加载后重试")

type SessionMemoryStore interface {
	EvidenceHistory(context.Context, int64, int64, int64, int) (model.MemorySourcePage, error)
	ReadSourceRange(context.Context, int64, int64, string, int, int) (model.MemorySource, error)
	Resolve(context.Context, int64, int64) (*model.SessionConversation, error)
	History(context.Context, int64, int64, int64, int) (model.SessionHistoryPage, error)
	Sources(context.Context, int64, int64, int64, int64, int) (model.MemorySourcePage, error)
	Load(context.Context, int64, int64) (*model.SessionMemory, error)
	SaveCAS(context.Context, int64, int64, int64, model.ChatLease, model.SessionMemory) (bool, error)
	Acquire(context.Context, int64, int64, string, time.Duration) (model.ChatLease, error)
	Renew(context.Context, int64, int64, model.ChatLease, time.Duration) (bool, error)
	Release(context.Context, int64, int64, model.ChatLease) error
	AppendLeasedMessage(context.Context, int64, int64, model.ChatLease, model.SessionMessage) (int64, error)
	Search(context.Context, int64, int64, string, int64, int) (model.SessionHistoryPage, error)
	ReadReferences(context.Context, int64, int64, []int64, []string) (model.MemorySourcePage, error)
}
type MemorySummarizer interface {
	Summarize(context.Context, model.MemorySummaryInput) (model.MemorySummaryResult, error)
}
type chatLeaseKey struct{}
type ChatLeaseScope struct {
	UserID, SessionID int64
	Lease             model.ChatLease
}

func WithChatLease(ctx context.Context, scope ChatLeaseScope) context.Context {
	return context.WithValue(ctx, chatLeaseKey{}, scope)
}
func CurrentChatLease(ctx context.Context) (ChatLeaseScope, bool) {
	v, ok := ctx.Value(chatLeaseKey{}).(ChatLeaseScope)
	return v, ok
}

var sessionCredentialText = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|secret|token|jwt|authorization)\s*["']?\s*[=:]\s*(?:(?:Bearer|Basic)\s+[a-zA-Z0-9._~+/=-]+|"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;"']+)`)

func RedactSessionText(text string) string {
	var parsed any
	if json.Unmarshal([]byte(text), &parsed) == nil {
		switch parsed.(type) {
		case map[string]any, []any:
			raw, _ := json.Marshal(redactSessionValue(parsed))
			return string(raw)
		}
	}
	return brainBearerText.ReplaceAllString(sessionCredentialText.ReplaceAllString(text, "$1=[REDACTED]"), "Bearer [REDACTED]")
}

// Apply text redaction to decoded leaves before JSON escaping can hide credentials.
func redactSessionValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if redacted, ok := redactBrainValue(map[string]any{k: val}).(map[string]any)[k].(string); ok && redacted == "[REDACTED]" {
				x[k] = redacted
			} else {
				x[k] = redactSessionValue(val)
			}
		}
	case []any:
		for i, val := range x {
			x[i] = redactSessionValue(val)
		}
	case string:
		return RedactSessionText(x)
	}
	return v
}
