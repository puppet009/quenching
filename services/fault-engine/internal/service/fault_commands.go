package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/repository"
	"reflect"
	"regexp"
	"time"
)

type FaultCommandStore interface {
	Reserve(context.Context, model.FaultCommand) (model.FaultCommand, model.FaultCommandResult, bool, error)
	Get(context.Context, string) (model.FaultCommand, model.FaultCommandResult, error)
	Save(context.Context, string, model.FaultCommandResult) error
	Combination(context.Context, string, []int64) (model.FaultCombination, error)
	ListCombinations(context.Context) ([]model.FaultCombination, error)
}
type FaultExecutionScope interface {
	Ready(context.Context) error
	ResolveScope(context.Context, model.InjectSpec) (model.InjectSpec, error)
}
type FaultCommandService struct {
	store    FaultCommandStore
	runtime  *RuntimeService
	scope    FaultExecutionScope
	revision func(context.Context, string) (string, error)
}

func NewFaultCommandService(store FaultCommandStore, runtime *RuntimeService, scope FaultExecutionScope, revision func(context.Context, string) (string, error)) *FaultCommandService {
	return &FaultCommandService{store, runtime, scope, revision}
}
func SameFaultCommand(a, b model.FaultCommand) bool {
	var x, y any
	if json.Unmarshal([]byte(a.ParametersJSON), &x) != nil || json.Unmarshal([]byte(b.ParametersJSON), &y) != nil {
		return false
	}
	a.ParametersJSON = ""
	b.ParametersJSON = ""
	return reflect.DeepEqual(a, b) && reflect.DeepEqual(x, y)
}
func VerifiedDirectorBinding(d *model.Defect, slug string) (model.TemplateBinding, error) {
	if d == nil || !d.IsActive {
		return model.TemplateBinding{}, errors.New("fault not enabled")
	}
	for _, b := range d.TemplateBindings {
		if b.TemplateSlug == slug && b.Status == "verified" {
			return b, checkAccepted(d, b)
		}
	}
	return model.TemplateBinding{}, errors.New("no verified template binding")
}
func (s *FaultCommandService) current(ctx context.Context, slug string, ids []int64) ([]*model.Defect, []model.TemplateBinding, string, error) {
	if s.revision == nil || s.scope == nil {
		return nil, nil, "", errors.New("fault execution not configured")
	}
	if err := s.scope.Ready(ctx); err != nil {
		return nil, nil, "", err
	}
	rev, err := s.revision(ctx, slug)
	if err != nil || rev == "" {
		return nil, nil, "", errors.New("template revision unavailable")
	}
	ds := make([]*model.Defect, 0, len(ids))
	bs := make([]model.TemplateBinding, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, nil, "", errors.New("invalid or duplicate defect")
		}
		seen[id] = true
		d, e := s.runtime.defectRepo.GetByID(ctx, id)
		if e != nil {
			return nil, nil, "", e
		}
		b, e := VerifiedDirectorBinding(d, slug)
		if e != nil {
			return nil, nil, "", e
		}
		if hash, _ := b.Evidence["template_digest"].(string); hash != rev {
			return nil, nil, "", errors.New("fault acceptance belongs to another template revision")
		}
		ds = append(ds, d)
		bs = append(bs, b)
	}
	return ds, bs, FaultAcceptanceDigest(rev, ds), nil
}
func (s *FaultCommandService) ListTrainable(ctx context.Context, slug string) ([]model.TrainableFault, error) {
	out := []model.TrainableFault{}
	if s.scope == nil || s.scope.Ready(ctx) != nil {
		return out, nil
	}
	active := true
	for page := 1; ; page++ {
		data, err := s.runtime.defectRepo.List(ctx, repository.DefectQuery{TemplateSlug: slug, IsActive: &active, Page: page, PageSize: 100})
		if err != nil {
			return nil, err
		}
		for _, d := range data.Items {
			_, _, digest, e := s.current(ctx, slug, []int64{d.ID})
			if e != nil {
				continue
			}
			out = append(out, model.TrainableFault{DefectID: d.ID, Code: d.Code, Name: d.Name, Difficulty: d.Difficulty, TemplateSlug: slug, Capabilities: []string{"pod_chaosblade"}, AcceptanceDigest: digest})
		}
		if page*100 >= data.Total {
			break
		}
	}
	return out, nil
}

var faultCommandID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

var ErrFaultCommandRejected = errors.New("fault command rejected before reservation")

func (s *FaultCommandService) Submit(ctx context.Context, c model.FaultCommand) (out model.FaultCommandResult, retErr error) {
	reserveAttempted := false
	defer func() {
		if retErr != nil && !reserveAttempted {
			retErr = fmt.Errorf("%w: %v", ErrFaultCommandRejected, retErr)
		}
	}()
	if !faultCommandID.MatchString(c.CommandID) || c.SessionID <= 0 || c.TemplateSlug == "" {
		return model.FaultCommandResult{}, errors.New("invalid command identity")
	}
	// Check durable identity before configuration: an old command must never reinject.
	if old, result, err := s.store.Get(ctx, c.CommandID); err == nil {
		if !SameFaultCommand(old, c) {
			return model.FaultCommandResult{}, errors.New("command identity collision")
		}
		return result, nil
	}
	var params map[string]any
	if json.Unmarshal([]byte(c.ParametersJSON), &params) != nil || params == nil || len(params) != 0 {
		return model.FaultCommandResult{}, errors.New("only accepted fault parameters supported")
	}
	var specs []model.InjectSpec
	snapshots := []map[string]any{}
	switch c.Action {
	case "inject_fault":
		if len(c.DefectIDs) < 1 || len(c.DefectIDs) > 2 || len(c.ExperimentUIDs) != 0 {
			return model.FaultCommandResult{}, errors.New("one or two accepted faults required")
		}
		ds, bs, digest, err := s.current(ctx, c.TemplateSlug, c.DefectIDs)
		if err != nil {
			return model.FaultCommandResult{}, err
		}
		for _, d := range ds {
			snapshots = append(snapshots, FaultReviewSnapshot(d))
		}
		if c.AcceptanceDigest != digest {
			return model.FaultCommandResult{}, errors.New("acceptance digest changed")
		}
		if len(ds) == 2 {
			combo, e := s.store.Combination(ctx, c.TemplateSlug, c.DefectIDs)
			if e != nil {
				return model.FaultCommandResult{}, e
			}
			if e = ValidateFaultCombination(combo, digest); e != nil {
				return model.FaultCommandResult{}, e
			}
		}
		if s.runtime.resolveTemplateTarget == nil {
			return model.FaultCommandResult{}, errors.New("sandbox target resolver unavailable")
		}
		for i, d := range ds {
			cfg := d.AmplifierConfig
			if len(bs[i].Overrides) > 0 {
				cfg = bs[i].Overrides
			}
			raw, _ := json.Marshal(cfg)
			scn, e := ParseScenario(raw)
			if e != nil {
				return model.FaultCommandResult{}, e
			}
			pod, e := s.runtime.resolveTemplateTarget(ctx, c.SessionID, c.TemplateSlug, bs[i].TargetWorkload)
			if e != nil {
				return model.FaultCommandResult{}, e
			}
			spec := scn.Spec
			spec.Kind = scn.Kind
			spec.TargetNamespace = fmt.Sprintf("sandbox-%d", c.SessionID)
			spec.TargetPod = pod
			spec.TemplateDigest, _ = bs[i].Evidence["template_digest"].(string)
			spec, e = s.scope.ResolveScope(ctx, spec)
			if e != nil {
				return model.FaultCommandResult{}, e
			}
			if _, e = BuildPodBladeArgs(spec); e != nil {
				return model.FaultCommandResult{}, e
			}
			specs = append(specs, PrepareScalingSpec(spec, d.DefectType == "resource"))
		}
	case "revoke_fault":
		if len(c.DefectIDs) != 0 || len(c.ExperimentUIDs) < 1 || len(c.ExperimentUIDs) > 2 {
			return model.FaultCommandResult{}, errors.New("invalid revoke scope")
		}
		for _, uid := range c.ExperimentUIDs {
			if _, e := s.runtime.expRepo.GetByUID(ctx, c.SessionID, uid); e != nil {
				return model.FaultCommandResult{}, e
			}
		}
	default:
		return model.FaultCommandResult{}, errors.New("unsupported fault action")
	}
	// Read-only preflight may reject before reservation. A hold is acquired only
	// by Injector, after its experiment (including hold ID) has been persisted.
	if s.runtime.inj.guard != nil {
		for _, spec := range specs {
			if resourceFault(spec) {
				if err := s.runtime.inj.guard.Check(ctx, spec.TargetNamespace); err != nil {
					return model.FaultCommandResult{}, err
				}
			}
		}
	}
	reserveAttempted = true
	old, result, fresh, err := s.store.Reserve(ctx, c)
	if err != nil {
		return model.FaultCommandResult{}, err
	}
	if !SameFaultCommand(old, c) {
		return model.FaultCommandResult{}, errors.New("command identity collision")
	}
	if !fresh {
		return result, nil
	}
	// Reservation commits before mutation. A crash leaves unknown, never a repeatable pending injection.
	result.Status = "unknown"
	executionStarted := time.Now().UTC()
	for _, spec := range specs {
		exp := &model.Experiment{SessionID: c.SessionID, Source: "director", Kind: spec.Kind, Target: targetDesc(spec), Spec: spec, Status: model.StatusCreated}
		if err = s.runtime.expRepo.Create(ctx, exp); err != nil {
			break
		}
		var injected *model.Experiment
		injected, err = s.runtime.inj.Inject(ctx, spec)
		if err != nil {
			break
		}
		result.ExperimentUIDs = append(result.ExperimentUIDs, injected.ExperimentUID)
		if err = s.store.Save(ctx, c.CommandID, result); err != nil {
			break
		}
		if err = s.runtime.expRepo.MarkRunning(ctx, exp.ID, injected.ExperimentUID, injected.Command); err != nil {
			break
		}
	}
	if c.Action == "revoke_fault" {
		for _, uid := range c.ExperimentUIDs {
			if err = s.runtime.RevokeOne(ctx, c.SessionID, uid); err != nil {
				break
			}
			result.ExperimentUIDs = append(result.ExperimentUIDs, uid)
		}
	}
	if err == nil {
		result.Status = "succeeded"
		deadline := executionStarted.Add(120 * time.Second)
		for _, spec := range specs {
			candidate := executionStarted.Add(time.Duration(spec.DurationSec) * time.Second)
			if candidate.Before(deadline) {
				deadline = candidate
			}
		}
		raw, _ := json.Marshal(map[string]any{"executed": true, "experiment_uids": result.ExperimentUIDs, "expires_at": deadline, "faults": snapshots})
		result.ResultJSON = string(raw)
	} else {
		result.ResultJSON = `{"reason":"execution outcome requires reconciliation","cleanup_pending":true}`
		if c.Action == "inject_fault" {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			for i, uid := range result.ExperimentUIDs {
				if i < len(specs) {
					_ = s.runtime.inj.Revoke(cleanup, specs[i], uid)
				}
			}
			cancel()
		}
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if e := s.store.Save(persist, c.CommandID, result); e != nil {
		return result, e
	}
	return result, nil
}
func (s *FaultCommandService) Get(ctx context.Context, id string) (model.FaultCommandResult, error) {
	_, r, e := s.store.Get(ctx, id)
	return r, e
}
func (s *FaultCommandService) ValidateCombination(ctx context.Context, c model.FaultCombination) error {
	if len(c.DefectIDs) != 2 {
		return errors.New("exactly two faults required")
	}
	if c.Status != "draft" && c.Status != "verified" {
		return errors.New("invalid combination status")
	}
	if c.Status == "verified" {
		_, _, digest, e := s.current(ctx, c.TemplateSlug, c.DefectIDs)
		if e != nil {
			return e
		}
		if e = ValidateFaultCombination(c, digest); e != nil {
			return e
		}
	}
	return nil
}
func (s *FaultCommandService) ListCombinations(ctx context.Context) ([]model.FaultCombination, error) {
	return s.store.ListCombinations(ctx)
}

func (s *FaultCommandService) PreviewCombination(ctx context.Context, slug string, ids []int64) (model.FaultCombination, error) {
	if len(ids) != 2 {
		return model.FaultCombination{}, errors.New("two faults required")
	}
	_, _, digest, err := s.current(ctx, slug, ids)
	if err != nil {
		return model.FaultCombination{}, err
	}
	return model.FaultCombination{TemplateSlug: slug, DefectIDs: ids, Status: "draft", AcceptanceDigest: digest}, nil
}

func (s *FaultCommandService) ListAcceptedCombinations(ctx context.Context, slug string) ([]model.FaultCombination, error) {
	all, e := s.store.ListCombinations(ctx)
	if e != nil {
		return nil, e
	}
	out := []model.FaultCombination{}
	for _, c := range all {
		if c.TemplateSlug != slug {
			continue
		}
		_, _, digest, e := s.current(ctx, slug, c.DefectIDs)
		if e == nil && ValidateFaultCombination(c, digest) == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// Capture only review metadata from the exact accepted fault revision used by this command.
func FaultReviewSnapshot(d *model.Defect) map[string]any {
	return map[string]any{"id": d.ID, "code": d.Code, "name": d.Name, "category": d.Category, "root_cause": d.RootCause, "description": d.Description, "provenance": d.Provenance}
}
