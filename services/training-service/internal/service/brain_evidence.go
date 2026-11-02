package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/training-service/internal/model"
	"regexp"
	"strings"
	"time"
)

type BrainEvidenceStore interface {
	SaveEvidence(context.Context, model.BrainEvidence) (string, error)
	ReadEvidence(context.Context, string) (*model.BrainEvidence, error)
}
type BrainEvidenceService struct{ store BrainEvidenceStore }

func NewBrainEvidenceService(s BrainEvidenceStore) *BrainEvidenceService {
	return &BrainEvidenceService{store: s}
}

var brainCredentialText = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|secret|token)\s*[=:]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;"']+)`)
var brainBearerText = regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9._~+/=-]+`)

func redactBrainValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			key := strings.ToLower(k)
			if key == "password" || key == "passwd" || key == "authorization" || key == "api_key" || key == "secret" || key == "jwt" || key == "token" || strings.HasSuffix(key, "_token") || strings.HasSuffix(key, "_password") || strings.HasSuffix(key, "_secret") {
				x[k] = "[REDACTED]"
			} else {
				x[k] = redactBrainValue(val)
			}
		}
		return x
	case []any:
		for i, val := range x {
			x[i] = redactBrainValue(val)
		}
		return x
	case string:
		var nested any
		if json.Unmarshal([]byte(x), &nested) == nil {
			switch nested.(type) {
			case map[string]any, []any:
				raw, _ := json.Marshal(redactBrainValue(nested))
				return string(raw)
			}
		}
		return brainBearerText.ReplaceAllString(brainCredentialText.ReplaceAllString(x, "$1=[REDACTED]"), "Bearer [REDACTED]")
	default:
		return v
	}
}
func NormalizeBrainEvidence(kind, raw string) (string, error) {
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil || payload == nil {
		return "", errors.New("evidence must be a JSON object")
	}
	payload = redactBrainValue(payload).(map[string]any)
	payload["trust"] = "untrusted_data"
	if kind == "terminal" {
		if _, ok := payload["exit_code"]; !ok {
			payload["execution_status"] = "unknown"
		} else {
			payload["execution_status"] = "completed"
		}
	}
	out, err := json.Marshal(payload)
	return string(out), err
}
func (s *BrainEvidenceService) Append(ctx context.Context, e model.BrainEvidence) (string, error) {
	if e.ID == "" || e.SessionID <= 0 || e.Kind == "" || e.Actor == "" {
		return "", errors.New("invalid evidence identity")
	}
	raw, err := NormalizeBrainEvidence(e.Kind, e.PayloadJSON)
	if err != nil {
		return "", err
	}
	e.PayloadJSON = raw
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	return s.store.SaveEvidence(ctx, e)
}
func (s *BrainEvidenceService) Read(ctx context.Context, sid int64, id string) (*model.BrainEvidence, error) {
	e, err := s.store.ReadEvidence(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil || e.SessionID != sid {
		return nil, errors.New("foreign evidence reference")
	}
	return e, nil
}
