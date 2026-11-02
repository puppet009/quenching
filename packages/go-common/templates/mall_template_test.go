package templates

import (
	"os"
	"path/filepath"
	"testing"
)

// Exercise the actual shipped template, rather than a duplicate test manifest.
func TestMallTemplateIsDiscoverableAndLoadable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "templates"))
	if err != nil {
		t.Fatal(err)
	}
	reg := Load(root)
	m, ok := reg.Get("mall")
	if !ok {
		t.Fatal("shipped mall template is missing or disabled")
	}
	found := false
	for _, item := range reg.List() {
		found = found || item.Slug == "mall"
	}
	if !found {
		t.Fatal("mall is absent from the template list")
	}
	if m.Name != "mall 电商交易实训" || len(m.LearningObjectives) != 6 || len(m.BusinessScenarios) != 5 || m.Cover == nil || len(m.Overview) == 0 || len(m.Audience) == 0 {
		t.Fatal("mall user presentation is missing project and training information")
	}
	if _, err := os.Stat(filepath.Join(reg.ChartPath(m), "Chart.yaml")); err != nil {
		t.Fatalf("mall chart cannot be loaded: %v", err)
	}
	if m.BehaviorFlow != nil {
		t.Fatal("loading stage must not start unfinished behavior simulation")
	}
	want := map[string]int{"frontend": 80, "mall-portal": 8085, "mysql": 3306, "redis": 6379, "rabbitmq": 5672, "mongo": 27017}
	if len(m.Services) != len(want) {
		t.Fatalf("got %d services, want %d", len(m.Services), len(want))
	}
	for _, service := range m.Services {
		port, exists := want[service.Name]
		if !exists || service.Port != port || service.Workload != service.Name {
			t.Fatalf("incorrect service mapping: %+v", service)
		}
		if service.DisplayName == "" || service.Description == "" {
			t.Fatalf("missing user-facing component introduction: %s", service.Name)
		}
	}
	for _, image := range m.Images {
		if _, err := os.Stat(filepath.Join(root, m.Slug, image.Context, image.Dockerfile)); err != nil {
			t.Fatalf("missing build recipe for %s: %v", image.Name, err)
		}
	}
}
