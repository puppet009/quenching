package service

import (
	"context"
	"encoding/json"
	"github.com/quenching/training-service/internal/model"
	"strings"
)

type StudentBrainObservation struct {
	EvidenceID string `json:"evidence_id"`
	Kind       string `json:"kind"`
	Summary    string `json:"summary"`
}
type StudentBrainContext struct {
	Phase              model.BrainPhase          `json:"phase"`
	HintLevel          int                       `json:"hint_level"`
	HintBudget         int                       `json:"hint_budget"`
	PublicObservations []StudentBrainObservation `json:"public_observations"`
}

func PublicBrainObservation(id, kind, raw string) StudentBrainObservation {
	var payload map[string]any
	_ = json.Unmarshal([]byte(raw), &payload)
	visible := map[string]any{}
	for _, key := range []string{"data", "direction", "path", "before", "after", "changed", "outcome", "operation_type", "operation_category", "result", "status", "business_verified", "execution_status", "truncated"} {
		if v, ok := payload[key]; ok {
			visible[key] = v
		}
	}
	safe, _ := json.Marshal(visible)
	summary := string(safe)
	if len(summary) > 2048 {
		summary = summary[:2048] + " [truncated]"
	}
	return StudentBrainObservation{id, kind, summary}
}

type StudentBrainContextReader interface {
	State(context.Context, int64) (*BrainSessionState, error)
	StudentEvidence(context.Context, int64) ([]model.BrainEvidence, error)
}
type StudentBrainContextService struct{ reader StudentBrainContextReader }

func NewStudentBrainContextService(r StudentBrainContextReader) *StudentBrainContextService {
	return &StudentBrainContextService{r}
}
func (s *StudentBrainContextService) BuildStudentBrainContext(ctx context.Context, sid int64) (*StudentBrainContext, error) {
	state, e := s.reader.State(ctx, sid)
	if e != nil {
		return nil, e
	}
	items, e := s.reader.StudentEvidence(ctx, sid)
	if e != nil {
		return nil, e
	}
	out := &StudentBrainContext{Phase: state.Phase, HintLevel: state.HintLevel, HintBudget: state.HintBudget, PublicObservations: []StudentBrainObservation{}}
	for _, v := range items {
		if v.Actor == "student" || v.Actor == "student_report" || v.Actor == "simulator" {
			out.PublicObservations = append(out.PublicObservations, PublicBrainObservation(v.ID, v.Kind, v.PayloadJSON))
		}
	}
	return out, nil
}
func StudentBrainContextJSON(c *StudentBrainContext) string {
	raw, e := json.Marshal(c)
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
