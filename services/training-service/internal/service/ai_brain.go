package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/proto/aiv1"
	"github.com/quenching/training-service/internal/model"
	"google.golang.org/grpc/metadata"
	"io"
	"time"
)

var brainActions = map[string]bool{
	"observe_session": true, "list_trainable_faults": true, "inspect_evidence": true,
	"run_user_behavior": true, "inject_fault": true, "revoke_fault": true, "verify_business": true,
	"set_hint_policy": true, "update_ability_assessment": true, "wait_for_event": true, "finish_training": true,
}

func decodeBrainDecision(raw string, obs model.BrainObservation) (*model.BrainDecision, error) {
	var decision model.BrainDecision
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decision); err != nil {
		return nil, fmt.Errorf("invalid brain decision: %w", err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing decision content")
	}
	if decision.DecisionID == "" || len(decision.DecisionID) > 128 || decision.ObservationVersion != obs.ObservationVersion || decision.LearningObjective == "" || decision.ReasonSummary == "" || decision.NextObservation == "" {
		return nil, errors.New("incomplete or stale brain decision")
	}
	allowed := false
	for _, t := range obs.AllowedTools {
		if t != nil && t.Name == decision.Action {
			allowed = true
		}
	}
	if !brainActions[decision.Action] || !allowed {
		return nil, errors.New("brain action not permitted")
	}
	var params map[string]any
	if json.Unmarshal([]byte(decision.ParametersJSON), &params) != nil || params == nil {
		return nil, errors.New("invalid action parameters")
	}
	refs := map[string]bool{}
	for _, ref := range obs.EvidenceRefs {
		refs[ref] = true
	}
	for _, ref := range decision.EvidenceRefs {
		if !refs[ref] {
			return nil, errors.New("foreign evidence reference")
		}
	}
	return &decision, nil
}

func (c *AIClient) DecideBrain(ctx context.Context, obs model.BrainObservation) (*model.BrainDecision, error) {
	if c.cfg.InternalToken == "" {
		return nil, errors.New("brain internal token not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-internal-token", c.cfg.InternalToken)
	request := &aiv1.BrainDecideRequest{SessionId: obs.SessionID, Phase: string(obs.Phase), ObservationVersion: obs.ObservationVersion, ContextJson: obs.ContextJSON, EvidenceRefs: obs.EvidenceRefs, AllowedTools: obs.AllowedTools, PreviousCalls: obs.PreviousCalls}
	for _, result := range obs.ToolResults {
		request.ToolResults = append(request.ToolResults, &aiv1.ToolResult{CallId: result.CommandID, ContentJson: result.ResultJSON, IsError: result.Status == "failed"})
	}
	resp, err := c.raw.BrainDecide(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("ai.BrainDecide: %w", err)
	}
	if resp.ActualModel != "qwen3.8-flash" || resp.Mocked {
		return nil, errors.New("director did not return actual qwen3.8-flash inference")
	}
	decision, err := decodeBrainDecision(resp.DecisionJson, obs)
	if err != nil {
		return nil, err
	}
	decision.ActualModel = resp.ActualModel
	decision.RequestID = resp.RequestId
	decision.PromptVersion = resp.PromptVersion
	decision.PromptTokens = resp.PromptTokens
	decision.CompletionTokens = resp.CompletionTokens
	decision.LatencyMS = resp.LatencyMs
	return decision, nil
}
