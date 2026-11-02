package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
	"os"
	"strings"
	"testing"
)

func TestTemplateRuntimeRejectsUnavailableAndForeignTargets(t *testing.T) {
	raw, err := os.ReadFile("../../content-service/catalog/mall-first-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog []model.Defect
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"unavailable", "missing_workload", "foreign_namespace", "foreign_pod", "unbound", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := newRuntimeFixture(t)
			d := catalog[0]
			d.IsActive = true
			d.TemplateBindings[0].Status = "verified"
			d.TemplateBindings[0].Evidence = model.JSONMap{"observed": "observed", "repaired": "repaired", "revoked": "revoked", "cleanup": "cleanup", "validated_at": "2026-10-04"}
			if mode == "unbound" {
				d.TemplateBindings = nil
			}
			if mode == "disabled" {
				d.IsActive = false
			}
			if err := f.defectRepo.Create(context.Background(), &d); err != nil {
				t.Fatal(err)
			}
			f.svc.SetTemplateResolver(func(context.Context, string) ([]string, error) {
				if mode == "unavailable" {
					return nil, fmt.Errorf("template unavailable")
				}
				if mode == "missing_workload" {
					return []string{"mysql"}, nil
				}
				return []string{"mall-portal"}, nil
			})
			f.svc.SetTemplateTargetResolver(func(context.Context, int64, string, string) (string, error) { return "mall-portal-abc", nil })
			req := service.StartScenarioReq{SessionID: 42, TemplateSlug: "mall", Code: d.Code}
			if mode == "foreign_namespace" {
				req.Namespace = "sandbox-43"
			}
			if mode == "foreign_pod" {
				req.Pod = "other-pod"
			}
			if _, err := f.svc.StartScenario(context.Background(), req); err == nil {
				t.Fatal("unsafe start accepted")
			} else if mode == "unavailable" && !strings.Contains(err.Error(), "unavailable") {
				t.Fatalf("wrong rejection: %v", err)
			}
		})
	}
}
