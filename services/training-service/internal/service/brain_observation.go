package service

import (
	"context"
	"encoding/json"
	"github.com/quenching/training-service/internal/model"
)

type BrainObservationReader interface {
	State(context.Context, int64) (*BrainSessionState, error)
	TemplateSlug(context.Context, int64) (string, error)
	ObservationData(context.Context, int64) (map[string]any, []string, error)
}

func BuildObservation(reader BrainObservationReader) BrainObservationBuilder {
	return func(ctx context.Context, sid int64) (model.BrainObservation, error) {
		state, err := reader.State(ctx, sid)
		if err != nil {
			return model.BrainObservation{}, err
		}
		data, refs, err := reader.ObservationData(ctx, sid)
		if err != nil {
			return model.BrainObservation{}, err
		}
		slug, err := reader.TemplateSlug(ctx, sid)
		if err != nil {
			return model.BrainObservation{}, err
		}
		data["template_slug"] = slug
		data["state"] = state
		raw, err := json.Marshal(data)
		if err != nil {
			return model.BrainObservation{}, err
		}
		return model.BrainObservation{SessionID: sid, Phase: state.Phase, ObservationVersion: state.ObservationVersion, ContextJSON: string(raw), EvidenceRefs: refs, AllowedTools: DirectorAllowedTools(*state)}, nil
	}
}
func DirectorAllowedTools(state BrainSessionState) []*model.BrainToolDef {
	empty := `{"type":"object","properties":{},"additionalProperties":false}`
	schemas := map[string]string{
		"update_ability_assessment": empty,
		"observe_session":           empty, "list_trainable_faults": empty, "verify_business": empty,
		"inspect_evidence":  `{"type":"object","properties":{"evidence_ids":{"type":"array","items":{"type":"string"}}},"required":["evidence_ids"],"additionalProperties":false}`,
		"run_user_behavior": `{"type":"object","properties":{"operation":{"type":"string","enum":["baseline","register","login","browse","cart","order","pay","verify"]},"profile":{"type":"string","enum":["steady","success","decline","delay","duplicate"]},"concurrency":{"type":"integer","maximum":1},"duration_seconds":{"type":"integer","maximum":0}},"required":["operation"],"additionalProperties":false}`,
		"inject_fault":      `{"type":"object","properties":{"defect_ids":{"type":"array","items":{"type":"integer"},"minItems":1,"maxItems":2},"parameters":{"type":"object","properties":{},"additionalProperties":false},"acceptance_digest":{"type":"string"}},"required":["defect_ids","acceptance_digest"],"additionalProperties":false}`,
		"revoke_fault":      `{"type":"object","properties":{"experiment_uids":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":2}},"required":["experiment_uids"],"additionalProperties":false}`,
		"set_hint_policy":   `{"type":"object","properties":{"hint_level":{"type":"integer","minimum":0,"maximum":3},"hint_budget":{"type":"integer","minimum":0,"maximum":10}},"required":["hint_level","hint_budget"],"additionalProperties":false}`,
		"wait_for_event":    `{"type":"object","properties":{"reason":{"type":"string"}},"required":["reason"],"additionalProperties":false}`,
		"finish_training":   `{"type":"object","properties":{"reason":{"type":"string"}},"required":["reason"],"additionalProperties":false}`,
	}
	actions := []string{"observe_session", "list_trainable_faults", "inspect_evidence", "run_user_behavior", "verify_business", "set_hint_policy", "update_ability_assessment", "wait_for_event", "finish_training"}
	if state.BaselineVerified && !state.ActiveChallenge && !state.PendingFaultRevoke {
		actions = append(actions, "inject_fault")
	}
	if state.ActiveChallenge || state.PendingFaultRevoke {
		actions = append(actions, "revoke_fault")
	}
	out := make([]*model.BrainToolDef, 0, len(actions))
	for _, action := range actions {
		out = append(out, &model.BrainToolDef{Name: action, Description: "受控训练工具；必须遵守当前证据和准入条件", ParametersJson: schemas[action]})
	}
	return out
}
