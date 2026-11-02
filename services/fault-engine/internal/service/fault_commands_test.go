package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/repository"
	"testing"
)

func TestNoVerifiedFaults(t *testing.T) {
	if _, err := VerifiedDirectorBinding(&model.Defect{IsActive: true}, "mall"); err == nil {
		t.Fatal("unverified fault advertised")
	}
}
func TestDuplicateInjection(t *testing.T) {
	a := model.FaultCommand{CommandID: "c1", SessionID: 1, TemplateSlug: "mall", Action: "inject_fault", DefectIDs: []int64{1}, ParametersJSON: "{}"}
	b := a
	if !SameFaultCommand(a, b) {
		t.Fatal("same intent not idempotent")
	}
	b.SessionID = 2
	if SameFaultCommand(a, b) {
		t.Fatal("foreign session reused command")
	}
}

type memoryFaultCommands struct {
	intent model.FaultCommand
	result model.FaultCommandResult
	exists bool
}

func (m *memoryFaultCommands) Get(context.Context, string) (model.FaultCommand, model.FaultCommandResult, error) {
	if !m.exists {
		return m.intent, m.result, errors.New("not found")
	}
	return m.intent, m.result, nil
}
func (m *memoryFaultCommands) Reserve(_ context.Context, c model.FaultCommand) (model.FaultCommand, model.FaultCommandResult, bool, error) {
	if m.exists {
		return m.intent, m.result, false, nil
	}
	m.exists = true
	m.intent = c
	m.result = model.FaultCommandResult{CommandID: c.CommandID, Status: "unknown"}
	return c, m.result, true, nil
}
func (m *memoryFaultCommands) Save(_ context.Context, _ string, v model.FaultCommandResult) error {
	m.result = v
	return nil
}
func (m *memoryFaultCommands) Combination(context.Context, string, []int64) (model.FaultCombination, error) {
	return model.FaultCombination{}, errors.New("not accepted")
}
func (m *memoryFaultCommands) SaveCombination(context.Context, model.FaultCombination) error {
	return nil
}
func (m *memoryFaultCommands) ListCombinations(context.Context) ([]model.FaultCombination, error) {
	return nil, nil
}

type readyFaultScope struct{}

func (readyFaultScope) Ready(context.Context) error { return nil }
func (readyFaultScope) ResolveScope(_ context.Context, s model.InjectSpec) (model.InjectSpec, error) {
	s.ExecNode = "node1"
	return s, nil
}

type countedFaultExecutor struct {
	calls int
	lose  bool
}

func (e *countedFaultExecutor) Run(context.Context, model.InjectSpec, []string) (*BladeExecResult, error) {
	e.calls++
	if e.lose {
		return nil, errors.New("response lost")
	}
	return &BladeExecResult{Raw: `{"code":200,"success":true,"result":"uid123"}`}, nil
}
func faultCommandFixture(t *testing.T, lose bool) (*FaultCommandService, model.FaultCommand, *countedFaultExecutor) {
	ctx := context.Background()
	dr := repository.NewInMemoryDefectRepo()
	er := repository.NewInMemoryExperimentRepo()
	var cfg model.JSONMap
	json.Unmarshal([]byte(`{"code":"accepted","kind":"cpu","spec":{"scope":"pod","cpu_percent":50,"duration_sec":60},"trigger":{"type":"delay","delay_seconds":1},"recovery":[{"metric":"cpu_usage","op":"lte","value":0.5}]}`), &cfg)
	d := &model.Defect{Code: "accepted", Name: "test", IsActive: true, AmplifierConfig: cfg, Provenance: model.JSONMap{"url": "https://example.com/incident/1", "kind": "incident"}, TemplateBindings: []model.TemplateBinding{{TemplateSlug: "mall", TargetWorkload: "mall-portal", Status: "verified", Executor: "pod_chaosblade", Evidence: model.JSONMap{"observed": "o", "repaired": "r", "revoked": "v", "cleanup": "c", "validated_at": "today", "template_digest": "revision"}}}}
	if e := dr.Create(ctx, d); e != nil {
		t.Fatal(e)
	}
	d, _ = dr.GetByID(ctx, d.ID)
	executor := &countedFaultExecutor{lose: lose}
	runtime := NewRuntimeService(ctx, executor, dr, er, NewSupervisorRegistry(), nil, RuntimeConfig{})
	t.Cleanup(runtime.Close)
	runtime.SetTemplateTargetResolver(func(context.Context, int64, string, string) (string, error) { return "portal-1", nil })
	s := NewFaultCommandService(&memoryFaultCommands{}, runtime, readyFaultScope{}, func(context.Context, string) (string, error) { return "revision", nil })
	c := model.FaultCommand{CommandID: "c1", SessionID: 7, TemplateSlug: "mall", Action: "inject_fault", DefectIDs: []int64{d.ID}, ParametersJSON: "{}", AcceptanceDigest: FaultAcceptanceDigest("revision", []*model.Defect{d})}
	return s, c, executor
}
func TestFaultCommandDuplicateDoesNotExecute(t *testing.T) {
	s, c, e := faultCommandFixture(t, false)
	first, err := s.Submit(context.Background(), c)
	if err != nil || first.Status != "succeeded" {
		t.Fatalf("first result: %+v %v", first, err)
	}
	again, err := s.Submit(context.Background(), c)
	if err != nil || again.Status != "succeeded" || e.calls != 1 {
		t.Fatalf("repeated injection: %d %+v %v", e.calls, again, err)
	}
	c.SessionID = 8
	if _, err = s.Submit(context.Background(), c); err == nil {
		t.Fatal("foreign command collision allowed")
	}
}
func TestFaultCommandUnknownNeverRetries(t *testing.T) {
	s, c, e := faultCommandFixture(t, true)
	first, err := s.Submit(context.Background(), c)
	if err != nil || first.Status != "unknown" {
		t.Fatalf("lost result: %+v %v", first, err)
	}
	again, err := s.Submit(context.Background(), c)
	if err != nil || again.Status != "unknown" || e.calls != 1 {
		t.Fatal("unknown execution repeated")
	}
}
func TestFaultCommandStaleRevisionRefused(t *testing.T) {
	s, c, e := faultCommandFixture(t, false)
	s.revision = func(context.Context, string) (string, error) { return "changed", nil }
	if _, err := s.Submit(context.Background(), c); err == nil || e.calls != 0 {
		t.Fatal("injected against stale acceptance")
	}
	list, err := s.ListTrainable(context.Background(), "mall")
	if err != nil || len(list) != 0 {
		t.Fatal("advertised stale fault")
	}
}

func TestFaultReviewSnapshotPreservesSourceWithoutExecutorSecrets(t *testing.T) {
	d := &model.Defect{ID: 1, RootCause: "cause", Provenance: model.JSONMap{"url": "https://example.org/postmortem"}, AmplifierConfig: model.JSONMap{"secret": "hidden"}}
	raw, _ := json.Marshal(FaultReviewSnapshot(d))
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["provenance"] == nil || got["root_cause"] != "cause" || got["amplifier_config"] != nil {
		t.Fatalf("bad snapshot %s", raw)
	}
}
