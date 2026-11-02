package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
	"testing"
)

func TestTemplateAcceptanceEditInvalidatesEvidence(t *testing.T) {
	ctx := context.Background()
	s := newCatalogTestService(repository.NewInMemoryDefectRepo())
	s.SetTemplateResolver(func(context.Context, string) ([]string, error) { return []string{"mall-portal"}, nil })
	var cfg model.JSONMap
	json.Unmarshal([]byte(`{"code":"accepted","kind":"cpu","spec":{"scope":"pod","cpu_percent":50,"duration_sec":60},"trigger":{"type":"delay","delay_seconds":1},"recovery":[{"metric":"cpu_usage","op":"lte","value":0.5}]}`), &cfg)
	d, err := s.CreateDefect(ctx, &CreateDefectReq{Code: "accepted", Name: "accepted", AmplifierConfig: cfg, Provenance: model.JSONMap{"kind": "incident", "url": "https://example.org/incident"}, TemplateBindings: []model.TemplateBinding{{Executor: "pod_chaosblade", TemplateSlug: "mall", TargetWorkload: "mall-portal", Status: "pending_validation"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AcceptBinding(ctx, d.ID, "mall", model.JSONMap{}); err == nil {
		t.Fatal("accepted missing evidence")
	}
	evidence := model.JSONMap{"observed": "observed", "repaired": "repaired", "revoked": "revoked", "cleanup": "cleanup", "validated_at": "2026-10-04"}
	if err = s.AcceptBinding(ctx, d.ID, "mall", evidence); err != nil {
		t.Fatal(err)
	}
	if err = s.SetActive(ctx, d.ID, true); err != nil {
		t.Fatal(err)
	}
	name := "changed"
	updated, err := s.UpdateDefect(ctx, d.ID, &UpdateDefectReq{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.IsActive || updated.TemplateBindings[0].Status != "pending_validation" || len(updated.TemplateBindings[0].Evidence) != 0 {
		t.Fatal("editing retained old acceptance")
	}
	if err = s.SetActive(ctx, d.ID, true); err == nil {
		t.Fatal("reenabled without new acceptance")
	}
}

func TestTemplateFaultCannotRemoveAcceptanceBoundary(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemoryDefectRepo()
	s := newCatalogTestService(repo)
	s.SetTemplateResolver(func(context.Context, string) ([]string, error) { return []string{"mall-portal"}, nil })
	d, err := s.CreateDefect(ctx, &CreateDefectReq{Code: "boundary", Name: "boundary", TemplateBindings: []model.TemplateBinding{{TemplateSlug: "mall", TargetWorkload: "mall-portal", Status: "draft"}}})
	if err != nil {
		t.Fatal(err)
	}
	empty := []model.TemplateBinding{}
	if _, err = s.UpdateDefect(ctx, d.ID, &UpdateDefectReq{TemplateBindings: &empty}); err == nil {
		t.Fatal("removed template acceptance boundary")
	}
	stored, _ := s.GetDefect(ctx, d.ID)
	if len(stored.TemplateBindings) != 1 {
		t.Fatal("failed update mutated repository")
	}
	if err = s.SetActive(ctx, d.ID, true); err == nil {
		t.Fatal("enabled unaccepted fault")
	}
}

func TestTemplateFaultCannotEnableBeforeValidation(t *testing.T) {
	s := newCatalogTestService(repository.NewInMemoryDefectRepo())
	s.SetTemplateResolver(func(context.Context, string) ([]string, error) { return []string{"mall-portal", "mysql"}, nil })
	d, err := s.CreateDefect(context.Background(), &CreateDefectReq{Code: "test", Name: "test", DefectType: "resource", Category: "cpu", Difficulty: "easy", TemplateBindings: []model.TemplateBinding{{TemplateSlug: "mall", TargetWorkload: "mall-portal", Status: "pending_validation"}}})
	if err != nil {
		t.Fatal(err)
	}
	if d.IsActive {
		t.Fatal("template fault activated without validation")
	}
	if s.SetActive(context.Background(), d.ID, true) == nil {
		t.Fatal("unverified fault was enabled")
	}
	_, err = s.CreateDefect(context.Background(), &CreateDefectReq{Code: "bad", Name: "bad", TemplateBindings: []model.TemplateBinding{{TemplateSlug: "mall", TargetWorkload: "host", Status: "draft"}}})
	if err == nil {
		t.Fatal("unknown workload accepted")
	}
	page, err := s.ListDefects(context.Background(), repository.DefectQuery{TemplateSlug: "mall"})
	if err != nil || page.Total != 1 {
		t.Fatalf("template filtering: %v %v", page, err)
	}
}

func newCatalogTestService(repo repository.DefectRepository) *DefectService {
	s := NewDefectService(repo)
	s.SetExecutionValidator(func(_ context.Context, raw json.RawMessage, pod bool) error {
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil {
			return fmt.Errorf("invalid JSON")
		}
		if v["kind"] == nil {
			return fmt.Errorf("kind required")
		}
		if pod && v["kind"] == "unsupported" {
			return fmt.Errorf("unsupported")
		}
		return nil
	})
	return s
}
