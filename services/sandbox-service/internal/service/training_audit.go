package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type TrainingAuditDelivery struct {
	ID    string
	Event []byte
}
type TrainingAuditStore interface {
	AppendAudit(context.Context, string, int64, string, []byte) error
	PendingAudits(context.Context) ([]TrainingAuditDelivery, error)
	MarkAuditDelivered(context.Context, string) error
}
type TrainingAuditService struct{ store TrainingAuditStore }

func NewTrainingAuditService(s TrainingAuditStore) *TrainingAuditService {
	return &TrainingAuditService{s}
}
func (a *TrainingAuditService) Append(ctx context.Context, sid int64, kind string, payload any) error {
	raw, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	id := "audit:" + uuid.NewString()
	event, e := BuildTrainingAuditEvent(id, sid, kind, string(raw))
	if e != nil {
		return e
	}
	bounded, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer done()
	return a.store.AppendAudit(bounded, id, sid, kind, event)
}

var auditCredentials = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|secret|token)\s*[=:]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;"']+)`)
var auditBearer = regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9._~+/=-]+`)

func redactAudit(value any) any {
	switch x := value.(type) {
	case map[string]any:
		for k, v := range x {
			key := strings.ToLower(k)
			if key == "password" || key == "passwd" || key == "authorization" || key == "secret" || key == "jwt" || key == "token" || key == "api_key" || strings.HasSuffix(key, "_token") || strings.HasSuffix(key, "_password") || strings.HasSuffix(key, "_secret") {
				x[k] = "[REDACTED]"
			} else {
				x[k] = redactAudit(v)
			}
		}
		return x
	case []any:
		for i, v := range x {
			x[i] = redactAudit(v)
		}
		return x
	case string:
		var nested any
		if (strings.HasPrefix(strings.TrimSpace(x), "{") || strings.HasPrefix(strings.TrimSpace(x), "[")) && json.Unmarshal([]byte(x), &nested) == nil {
			raw, _ := json.Marshal(redactAudit(nested))
			return string(raw)
		}
		return auditBearer.ReplaceAllString(auditCredentials.ReplaceAllString(x, "$1=[REDACTED]"), "Bearer [REDACTED]")
	default:
		return value
	}
}
func BuildTrainingAuditEvent(id string, sid int64, kind, raw string) ([]byte, error) {
	if id == "" || sid <= 0 || kind == "" {
		return nil, errors.New("invalid audit identity")
	}
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil || payload == nil {
		return nil, errors.New("invalid audit payload")
	}
	payload = redactAudit(payload).(map[string]any)
	eventKind := "student_operation"
	if kind == "terminal" {
		eventKind = "terminal_record"
		payload["execution_status"] = "unknown"
	}
	payload["trust"] = "untrusted_data"
	safe, _ := json.Marshal(payload)
	return json.Marshal(map[string]any{"event": map[string]any{"session_id": sid, "source": "sandbox_audit", "event_id": id, "kind": eventKind, "evidence_refs": []string{id}}, "evidence": []any{map[string]any{"id": id, "session_id": sid, "kind": kind, "actor": "student", "occurred_at": time.Now().UTC(), "payload_json": string(safe)}}})
}
func RunTrainingAuditOutbox(ctx context.Context, s TrainingAuditStore, base, token string) {
	if base == "" || token == "" {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		items, e := s.PendingAudits(ctx)
		if e != nil {
			continue
		}
		for _, item := range items {
			req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/internal/v1/brain/events", bytes.NewReader(item.Event))
			if e != nil {
				continue
			}
			req.Header.Set("X-Internal-Token", token)
			req.Header.Set("Content-Type", "application/json")
			res, e := client.Do(req)
			if e != nil {
				continue
			}
			var ack struct {
				Code int `json:"code"`
			}
			decodeErr := json.NewDecoder(res.Body).Decode(&ack)
			res.Body.Close()
			if res.StatusCode == 200 && decodeErr == nil && ack.Code == 0 {
				_ = s.MarkAuditDelivered(ctx, item.ID)
			}
		}
	}
}

func (a *TrainingAuditService) AssertWorkspaceOpen(ctx context.Context, sid int64) error {
	store, ok := a.store.(interface {
		WorkspaceOpen(context.Context, int64) (bool, error)
	})
	if !ok {
		return errors.New("workspace lifecycle unavailable")
	}
	open, err := store.WorkspaceOpen(ctx, sid)
	if err != nil {
		return err
	}
	if !open {
		return errors.New("workspace is closed")
	}
	return nil
}
