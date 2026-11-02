package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/training-service/internal/model"
	"io"
)

type BrainToolParameters struct {
	Operation        string         `json:"operation,omitempty"`
	Profile          string         `json:"profile,omitempty"`
	Concurrency      int            `json:"concurrency,omitempty"`
	DurationSeconds  int            `json:"duration_seconds,omitempty"`
	DefectIDs        []int64        `json:"defect_ids,omitempty"`
	Parameters       map[string]any `json:"parameters,omitempty"`
	AcceptanceDigest string         `json:"acceptance_digest,omitempty"`
	ExperimentUIDs   []string       `json:"experiment_uids,omitempty"`
	EvidenceIDs      []string       `json:"evidence_ids,omitempty"`
	HintLevel        int            `json:"hint_level,omitempty"`
	HintBudget       int            `json:"hint_budget,omitempty"`
	Reason           string         `json:"reason,omitempty"`
}

func ValidateBrainToolParameters(cmd model.BrainCommand) (*BrainToolParameters, error) {
	if cmd.SessionID <= 0 || !brainActions[cmd.Action] {
		return nil, errors.New("unauthorized brain action")
	}
	fields := map[string][]string{
		"observe_session": {}, "list_trainable_faults": {}, "inspect_evidence": {"evidence_ids"},
		"run_user_behavior": {"operation", "profile", "concurrency", "duration_seconds"},
		"inject_fault":      {"defect_ids", "parameters", "acceptance_digest"}, "revoke_fault": {"experiment_uids"},
		"verify_business": {}, "set_hint_policy": {"hint_level", "hint_budget"}, "update_ability_assessment": {}, "wait_for_event": {"reason"}, "finish_training": {"reason"},
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(cmd.ParametersJSON), &raw) != nil || raw == nil {
		return nil, errors.New("invalid parameters")
	}
	allowed := map[string]bool{}
	for _, field := range fields[cmd.Action] {
		allowed[field] = true
	}
	for field := range raw {
		if !allowed[field] {
			return nil, errors.New("unexpected tool parameter")
		}
	}
	var p BrainToolParameters
	dec := json.NewDecoder(bytes.NewBufferString(cmd.ParametersJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing parameters")
	}
	if p.Concurrency < 0 || p.Concurrency > 20 || p.DurationSeconds < 0 || p.DurationSeconds > 120 {
		return nil, errors.New("behavior budget exceeded")
	}
	if cmd.Action == "run_user_behavior" {
		switch p.Operation {
		case "baseline", "register", "login", "browse", "cart", "order", "pay", "verify":
		default:
			return nil, errors.New("unknown business operation")
		}
	}
	if cmd.Action == "inject_fault" {
		if len(p.DefectIDs) < 1 || len(p.DefectIDs) > 2 {
			return nil, errors.New("invalid fault count")
		}
		seen := map[int64]bool{}
		for _, id := range p.DefectIDs {
			if id <= 0 || seen[id] {
				return nil, errors.New("invalid duplicate fault")
			}
			seen[id] = true
		}
	}
	if cmd.Action == "set_hint_policy" && (p.HintLevel < 0 || p.HintLevel > 3 || p.HintBudget < 0 || p.HintBudget > 10) {
		return nil, errors.New("invalid hint policy")
	}
	return &p, nil
}

type BrainCommandGuard interface {
	CheckCommand(context.Context, model.BrainCommand) error
}
type BrainToolHandler func(context.Context, model.BrainCommand, *BrainToolParameters) (model.BrainToolResult, error)
type BrainToolExecutor struct {
	guard    BrainCommandGuard
	handlers map[string]BrainToolHandler
}

func NewBrainToolExecutor(g BrainCommandGuard) *BrainToolExecutor {
	return &BrainToolExecutor{guard: g, handlers: map[string]BrainToolHandler{}}
}
func (e *BrainToolExecutor) Register(action string, h BrainToolHandler) {
	if brainActions[action] && h != nil {
		e.handlers[action] = h
	}
}
func (e *BrainToolExecutor) Execute(ctx context.Context, cmd model.BrainCommand) (model.BrainToolResult, error) {
	p, err := ValidateBrainToolParameters(cmd)
	if err != nil {
		return model.BrainToolResult{}, err
	}
	if e.guard == nil {
		return model.BrainToolResult{}, errors.New("command guard unavailable")
	}
	if err = e.guard.CheckCommand(ctx, cmd); err != nil {
		return model.BrainToolResult{}, err
	}
	h := e.handlers[cmd.Action]
	if h == nil {
		return model.BrainToolResult{}, errors.New("tool capability unavailable")
	}
	result, err := h(ctx, cmd, p)
	if err != nil {
		return result, err
	}
	if result.CommandID != cmd.CommandID {
		return model.BrainToolResult{}, errors.New("tool result identity mismatch")
	}
	return result, nil
}
