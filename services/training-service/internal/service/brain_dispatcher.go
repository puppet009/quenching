package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/training-service/internal/model"
	"time"
)

type BrainDispatchStore interface {
	BrainStore
	ClaimCommand(context.Context, model.BrainLease, model.BrainCommand) error
	ApplyResult(context.Context, model.BrainLease, model.BrainCommand, model.BrainToolResult) error
	WakeSessions(context.Context) ([]int64, error)
	TemplateSlug(context.Context, int64) (string, error)
	SetHintPolicy(context.Context, model.BrainLease, int, int) error
	Freeze(context.Context, int64, string) error
}
type BrainDispatcher struct {
	store    BrainDispatchStore
	tools    *BrainToolExecutor
	domains  *BrainDomainClients
	director *BrainDirector
}

func NewBrainDispatcher(s BrainDispatchStore, t *BrainToolExecutor, d *BrainDomainClients) *BrainDispatcher {
	return &BrainDispatcher{store: s, tools: t, domains: d}
}
func (d *BrainDispatcher) SetDirector(director *BrainDirector) { d.director = director }
func (d *BrainDispatcher) Recover(ctx context.Context, l model.BrainLease) (bool, error) {
	commands, err := d.store.PendingCommands(ctx, l.SessionID)
	if err != nil {
		return false, err
	}
	for _, c := range commands {
		if BrainRecoveryAction(c) == "dispatch" {
			res, e := d.Execute(ctx, l, c)
			if e != nil {
				return true, e
			}
			if res.Status != "succeeded" && res.Status != "failed" {
				return true, nil
			}
			continue
		}
		// A submitted local action has no remote status endpoint. Never repeat a mutation after a crash.
		if c.Domain == "training" {
			if c.Action == "observe_session" || c.Action == "list_trainable_faults" || c.Action == "inspect_evidence" {
				res, e := d.tools.Execute(WithBrainLease(ctx, l), c)
				if e != nil {
					return true, e
				}
				if e = d.store.ApplyResult(ctx, l, c, res); e != nil {
					return true, e
				}
				continue
			}
			// A local mutation may already have happened. Do not replay it or claim success.
			// Persist a terminal uncertainty record so the session can resume on a later event.
			res := model.BrainToolResult{CommandID: c.CommandID, Status: "failed", ResultJSON: `{"reason":"local action outcome unconfirmed after restart; inspect current state before a new action","outcome_unconfirmed":true}`}
			if e := d.store.ApplyResult(ctx, l, c, res); e != nil {
				return true, e
			}
			return true, nil
		}
		res, e := d.domains.GetCommand(ctx, c.Domain, c.CommandID)
		if e != nil {
			return true, nil
		}
		if e = d.store.ApplyResult(ctx, l, c, res); e != nil {
			return true, e
		}
		if res.Status != "succeeded" && res.Status != "failed" {
			return true, nil
		}
	}
	return false, nil
}
func (d *BrainDispatcher) Execute(ctx context.Context, l model.BrainLease, c model.BrainCommand) (model.BrainToolResult, error) {
	if e := d.store.ClaimCommand(ctx, l, c); e != nil {
		return model.BrainToolResult{}, e
	}
	res, e := d.tools.Execute(WithBrainLease(ctx, l), c)
	if e != nil {
		// Local validation failures precede remote mutation. Ambiguous network submissions already return unknown.
		if res.Status == "" {
			res = model.BrainToolResult{CommandID: c.CommandID, Status: "failed", ResultJSON: `{"reason":"tool unavailable or validation refused"}`}
		}
	}
	if err := d.store.ApplyResult(ctx, l, c, res); err != nil {
		return res, err
	}
	return res, nil
}
func (d *BrainDispatcher) Resume(ctx context.Context) error {
	if d.director == nil {
		return errors.New("director not assembled")
	}
	sessions, err := d.store.WakeSessions(ctx)
	if err != nil {
		return err
	}
	for _, sid := range sessions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = d.director.Run(ctx, sid)
		if err != nil && !errors.Is(err, ErrBrainConflict) {
			continue
		}
	}
	return nil
}
func (d *BrainDispatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		_ = d.Resume(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func RegisterBrainRuntimeTools(executor *BrainToolExecutor, store BrainDispatchStore, reader BrainObservationReader, evidence *BrainEvidenceService, domains *BrainDomainClients) {
	result := func(c model.BrainCommand, data any) (model.BrainToolResult, error) {
		raw, e := json.Marshal(data)
		return model.BrainToolResult{CommandID: c.CommandID, Status: "succeeded", ResultJSON: string(raw)}, e
	}
	executor.Register("observe_session", func(ctx context.Context, c model.BrainCommand, _ *BrainToolParameters) (model.BrainToolResult, error) {
		o, e := BuildObservation(reader)(ctx, c.SessionID)
		if e != nil {
			return model.BrainToolResult{}, e
		}
		return result(c, o)
	})
	executor.Register("inspect_evidence", func(ctx context.Context, c model.BrainCommand, p *BrainToolParameters) (model.BrainToolResult, error) {
		out := []*model.BrainEvidence{}
		for _, id := range p.EvidenceIDs {
			v, e := evidence.Read(ctx, c.SessionID, id)
			if e != nil {
				return model.BrainToolResult{}, e
			}
			out = append(out, v)
		}
		return result(c, out)
	})
	executor.Register("list_trainable_faults", func(ctx context.Context, c model.BrainCommand, _ *BrainToolParameters) (model.BrainToolResult, error) {
		slug, e := store.TemplateSlug(ctx, c.SessionID)
		if e != nil {
			return model.BrainToolResult{}, e
		}
		data, e := domains.ListTrainableFaults(ctx, slug)
		if e != nil {
			return model.BrainToolResult{}, e
		}
		return result(c, data)
	})
	behavior := func(ctx context.Context, c model.BrainCommand, p *BrainToolParameters) (model.BrainToolResult, error) {
		if p.Concurrency > 1 || p.DurationSeconds != 0 {
			return model.BrainToolResult{}, errors.New("multi-actor load profile unavailable")
		}
		slug, e := store.TemplateSlug(ctx, c.SessionID)
		if e != nil {
			return model.BrainToolResult{}, e
		}
		operation := p.Operation
		if c.Action == "verify_business" {
			operation = "verify"
		}
		params, _ := json.Marshal(map[string]any{"profile": p.Profile, "concurrency": p.Concurrency, "duration_seconds": p.DurationSeconds})
		return domains.Submit(ctx, c, map[string]any{"command_id": c.CommandID, "session_id": c.SessionID, "template_slug": slug, "operation": operation, "parameters_json": string(params)})
	}
	executor.Register("run_user_behavior", behavior)
	executor.Register("verify_business", behavior)
	fault := func(ctx context.Context, c model.BrainCommand, p *BrainToolParameters) (model.BrainToolResult, error) {
		slug, e := store.TemplateSlug(ctx, c.SessionID)
		if e != nil {
			return model.BrainToolResult{}, e
		}
		if c.Action == "inject_fault" {
			state, e := reader.State(ctx, c.SessionID)
			if e != nil {
				return model.BrainToolResult{}, e
			}
			if !state.BaselineVerified || state.ActiveChallenge || state.PendingFaultRevoke {
				return model.BrainToolResult{}, ErrBrainConflict
			}
			catalog, e := domains.ListTrainableFaults(ctx, slug)
			if e != nil {
				return model.BrainToolResult{}, e
			}
			var view struct {
				Faults []BrainTrainableFault `json:"faults"`
			}
			if e = json.Unmarshal(catalog, &view); e != nil {
				return model.BrainToolResult{}, e
			}
			if e = ValidateCalibrationFaults(state.CurrentChallenge, p.DefectIDs, view.Faults); e != nil {
				return model.BrainToolResult{}, e
			}
		}
		params := p.Parameters
		if params == nil {
			params = map[string]any{}
		}
		raw, _ := json.Marshal(params)
		return domains.Submit(ctx, c, map[string]any{"command_id": c.CommandID, "session_id": c.SessionID, "template_slug": slug, "action": c.Action, "defect_ids": p.DefectIDs, "parameters_json": string(raw), "acceptance_digest": p.AcceptanceDigest, "experiment_uids": p.ExperimentUIDs})
	}
	executor.Register("inject_fault", fault)
	executor.Register("revoke_fault", fault)
	executor.Register("set_hint_policy", func(ctx context.Context, c model.BrainCommand, p *BrainToolParameters) (model.BrainToolResult, error) {
		l, ok := BrainLeaseFromContext(ctx)
		if !ok {
			return model.BrainToolResult{}, ErrBrainConflict
		}
		if e := store.SetHintPolicy(ctx, l, p.HintLevel, p.HintBudget); e != nil {
			return model.BrainToolResult{}, e
		}
		return result(c, map[string]any{"hint_level": p.HintLevel, "hint_budget": p.HintBudget})
	})
	executor.Register("finish_training", func(ctx context.Context, c model.BrainCommand, p *BrainToolParameters) (model.BrainToolResult, error) {
		return result(c, map[string]any{"finish_requested": true, "reason": fmt.Sprintf("%s", p.Reason)})
	})
}

type BrainTrainableFault struct {
	DefectID   int64  `json:"defect_id"`
	Difficulty string `json:"difficulty"`
}

func ValidateCalibrationFaults(current int, ids []int64, catalog []BrainTrainableFault) error {
	if current != 0 {
		return nil
	}
	if len(ids) != 1 {
		return errors.New("first calibration requires one easy fault")
	}
	for _, f := range catalog {
		if f.DefectID == ids[0] && f.Difficulty == "easy" {
			return nil
		}
	}
	return errors.New("first calibration requires an accepted easy fault")
}
