package service

import (
	"encoding/json"
	"github.com/quenching/content-service/internal/model"
	"os"
	"testing"
)

func TestMallCatalogCoverageAndIsolation(t *testing.T) {
	raw, err := os.ReadFile("../../catalog/mall-first-batch.json")
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
		if err := validateFields(&d); err != nil {
			t.Fatal(err)
		}
		if len(d.TemplateBindings) != 1 || d.TemplateBindings[0].TemplateSlug != "mall" || d.TemplateBindings[0].Status == "verified" {
			t.Fatal("invalid binding")
		}
		if d.Provenance["url"] == nil || d.Provenance["reproduction_difference"] == nil {
			t.Fatal("missing provenance")
		}
		b := d.TemplateBindings[0]
		if b.Executor != "pod_chaosblade" && b.Status != "draft" {
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
