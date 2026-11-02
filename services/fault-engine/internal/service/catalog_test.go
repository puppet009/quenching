package service

import (
	"encoding/json"
	"github.com/quenching/fault-engine/internal/model"
	"os"
	"testing"
)

func TestMallCatalogCoverageAndIsolation(t *testing.T) {
	raw, err := os.ReadFile("../../../content-service/catalog/mall-first-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	var defs []model.Defect
	if err = json.Unmarshal(raw, &defs); err != nil {
		t.Fatal(err)
	}
	if len(defs) != 8 {
		t.Fatalf("expected eight categories, got %d", len(defs))
	}
	cats, types, codes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range defs {
		if d.IsActive || d.ProjectID != 0 || codes[d.Code] {
			t.Fatalf("unsafe/duplicate catalog entry: %s", d.Code)
		}
		codes[d.Code] = true
		cats[d.Category] = true
		types[d.DefectType] = true
		if len(d.TemplateBindings) != 1 || d.TemplateBindings[0].TemplateSlug != "mall" || d.TemplateBindings[0].Status == "verified" {
			t.Fatal("invalid binding")
		}
		if d.Provenance["url"] == nil || d.Provenance["reproduction_difference"] == nil {
			t.Fatal("missing provenance")
		}
		b := d.TemplateBindings[0]
		if b.Executor == "pod_chaosblade" {
			cfg, _ := json.Marshal(d.AmplifierConfig)
			scn, err := ParseScenario(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if scn.Spec.Scope != "pod" {
				t.Fatal("host scope")
			}
			spec := scn.Spec
			spec.Kind = scn.Kind
			spec.TargetNamespace = "sandbox-9"
			spec.TargetPod = "mall-test"
			if _, err := BuildBladeCreateArgs(spec); err != nil {
				t.Fatal(err)
			}
		} else if b.Status != "draft" {
			t.Fatal("unsupported adapter must stay draft")
		}
	}
	for _, cat := range []string{"database", "cache", "mq", "network", "cpu", "memory", "disk", "threadpool"} {
		if !cats[cat] {
			t.Fatalf("missing %s", cat)
		}
	}
	if len(types) != 4 {
		t.Fatalf("missing defect types: %v", types)
	}
}
