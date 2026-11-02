package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/proto/aiv1"
	"github.com/quenching/training-service/internal/model"
	"google.golang.org/grpc/metadata"
	"time"
)

var AbilityDimensions = []string{"localization", "evidence", "mechanism", "repair", "regression", "architecture", "hint_dependence"}

type BrainChallengeFact struct {
	Status    string  `json:"status"`
	DefectIDs []int64 `json:"defect_ids"`
}

func CountVerifiedChallenges(facts []BrainChallengeFact) int {
	n := 0
	for _, f := range facts {
		if f.Status == "verified_repair" {
			n++
		}
	}
	return n
}
func GuardAbilityPromotion(a *model.AbilityAssessment, verified, distinct int) {
	sufficient := verified >= 2 && distinct >= 2 && a.Sufficiency == "sufficient" && len(a.Dimensions) == 7
	for _, key := range AbilityDimensions {
		v, ok := a.Dimensions[key]
		if !ok || v.Level == "unknown" || v.Confidence < 0.7 || len(v.EvidenceRefs) == 0 {
			sufficient = false
		}
	}
	if !sufficient {
		a.OverallLevel = "unknown"
		if a.Sufficiency == "sufficient" {
			a.Sufficiency = "partial"
		}
	}
}
func ValidateAbilityAssessment(a *model.AbilityAssessment, refs []string) error {
	if a == nil || len(a.Dimensions) != 7 {
		return errors.New("seven ability dimensions required")
	}
	known := map[string]bool{}
	for _, ref := range refs {
		known[ref] = true
	}
	for _, ref := range a.EvidenceRefs {
		if !known[ref] {
			return errors.New("foreign assessment evidence")
		}
	}
	for _, key := range AbilityDimensions {
		d, ok := a.Dimensions[key]
		if !ok || d.Confidence < 0 || d.Confidence > 1 {
			return errors.New("invalid ability dimension")
		}
		switch d.Level {
		case "unknown":
			if d.Confidence != 0 {
				return errors.New("unknown dimension must have confidence zero")
			}
		case "junior", "intermediate", "advanced":
			if len(d.EvidenceRefs) == 0 {
				return errors.New("observed dimension requires evidence")
			}
		default:
			return errors.New("invalid ability level")
		}
		for _, ref := range d.EvidenceRefs {
			if !known[ref] {
				return errors.New("foreign dimension evidence")
			}
		}
	}
	return nil
}

type AbilityStore interface {
	BrainObservationReader
	AssessmentVersion(context.Context, int64) (int64, error)
	SaveAssessment(context.Context, *model.AbilityAssessment) error
}
type AbilityInference interface {
	AssessAbility(context.Context, int64, int64, string, []string) (*model.AbilityAssessment, error)
}
type AbilityAssessmentService struct {
	store AbilityStore
	ai    AbilityInference
}

func NewAbilityAssessmentService(s AbilityStore, ai AbilityInference) *AbilityAssessmentService {
	return &AbilityAssessmentService{s, ai}
}
func (s *AbilityAssessmentService) Assess(ctx context.Context, sid int64) (*model.AbilityAssessment, error) {
	facts, refs, e := s.store.ObservationData(ctx, sid)
	if e != nil {
		return nil, e
	}
	version, e := s.store.AssessmentVersion(ctx, sid)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(facts)
	if e != nil {
		return nil, e
	}
	a, e := s.ai.AssessAbility(ctx, sid, version+1, string(raw), refs)
	if e != nil {
		return nil, e
	}
	if a.SessionID != sid || a.Version != version+1 {
		return nil, ErrBrainConflict
	}
	if e = ValidateAbilityAssessment(a, refs); e != nil {
		return nil, e
	}
	var challenges []BrainChallengeFact
	encoded, _ := json.Marshal(facts["challenges"])
	_ = json.Unmarshal(encoded, &challenges)
	distinct := map[int64]bool{}
	for _, c := range challenges {
		if c.Status == "verified_repair" {
			for _, id := range c.DefectIDs {
				distinct[id] = true
			}
		}
	}
	GuardAbilityPromotion(a, CountVerifiedChallenges(challenges), len(distinct))
	if e = s.store.SaveAssessment(ctx, a); e != nil {
		return nil, e
	}
	return a, nil
}
func (c *AIClient) AssessAbility(parent context.Context, sid, version int64, raw string, refs []string) (*model.AbilityAssessment, error) {
	ctx, cancel := context.WithTimeout(parent, 95*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-internal-token", c.cfg.InternalToken)
	if c.cfg.InternalToken == "" {
		return nil, errors.New("assessment internal token unavailable")
	}
	res, e := c.raw.AssessAbility(ctx, &aiv1.AssessAbilityRequest{SessionId: sid, Version: version, EvidenceJson: raw, EvidenceRefs: refs})
	if e != nil {
		return nil, e
	}
	if res.Mocked || res.ActualModel != "qwen3.8-flash" {
		return nil, errors.New("real qwen3.8-flash assessment required")
	}
	var a model.AbilityAssessment
	if e = json.Unmarshal([]byte(res.AssessmentJson), &a); e != nil {
		return nil, e
	}
	a.Model = res.ActualModel
	a.RequestID = res.RequestId
	a.PromptVersion = res.PromptVersion
	a.PromptTokens = res.PromptTokens
	a.CompletionTokens = res.CompletionTokens
	a.LatencyMS = res.LatencyMs
	return &a, nil
}
