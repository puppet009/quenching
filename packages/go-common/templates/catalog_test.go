package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogPresentationDoesNotChangeRuntimeContractOrDigest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "mall", validManifest)
	before, _ := Load(root).Get("mall")
	path := filepath.Join(root, "mall", "catalog.yaml")
	content := "name: 电商交易实训\ndescription: 围绕订单链路开展排查练习\nlearning_objectives: [分析数据库与缓存依赖]\nbusiness_scenarios: [购物车与订单处理]\nservices:\n  user-service:\n    display_name: 用户入口\n    description: 接收用户请求\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	after, ok := Load(root).Get("mall")
	if !ok || after.Name != "电商交易实训" || after.Description != "围绕订单链路开展排查练习" {
		t.Fatalf("public presentation not loaded: %+v", after)
	}
	if after.ArtifactDigest != before.ArtifactDigest || after.Chart != before.Chart || after.IsEnabled() != before.IsEnabled() {
		t.Fatal("display text changed deployment or acceptance identity")
	}
	if after.Services[0].Name != before.Services[0].Name || after.Services[0].Workload != before.Services[0].Workload || after.Services[0].Port != before.Services[0].Port || after.Services[0].Role != before.Services[0].Role {
		t.Fatal("display text changed a runtime service")
	}
	raw, _ := json.Marshal(after)
	for _, value := range []string{"learning_objectives", "business_scenarios", "用户入口", "接收用户请求"} {
		if !strings.Contains(string(raw), value) {
			t.Fatalf("presentation absent from API contract: %s", value)
		}
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(content, "实训", "训练")), 0644); err != nil {
		t.Fatal(err)
	}
	changed, _ := Load(root).Get("mall")
	if changed.Name == after.Name || changed.ArtifactDigest != before.ArtifactDigest {
		t.Fatal("editing catalog text must not invalidate fault acceptance")
	}
}

func TestCatalogCannotEnableOrReconfigureATemplate(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "mall", validManifest+"\nenabled: false\n")
	if err := os.WriteFile(filepath.Join(root, "mall", "catalog.yaml"), []byte("name: 上架项目\nenabled: true\nchart: other\nservices:\n  user-service:\n    port: 9999\n"), 0644); err != nil {
		t.Fatal(err)
	}
	registry := Load(root)
	if len(registry.List()) != 0 {
		t.Fatal("catalog enabled an unavailable template")
	}
	if _, ok := registry.Get("mall"); ok {
		t.Fatal("catalog bypassed the template gate")
	}
}

func TestMalformedCatalogDoesNotBreakLegacyTemplateLoading(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "mall", validManifest)
	if err := os.WriteFile(filepath.Join(root, "mall", "catalog.yaml"), []byte("name: ["), 0644); err != nil {
		t.Fatal(err)
	}
	m, ok := Load(root).Get("mall")
	if !ok || m.Name != "电商交易系统" {
		t.Fatal("optional presentation broke a previously valid template")
	}
}

func TestInvalidCatalogIsRejectedWithoutPartialChanges(t *testing.T) {
	for _, content := range []string{
		"name: 新名称\ndescription: 新简介\nenabled: true\n",
		"name: 新名称\ndescription: 新简介\nservices:\n  missing-service:\n    display_name: 不存在的组件\n",
	} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			writeManifest(t, root, "mall", validManifest)
			before, _ := Load(root).Get("mall")
			if err := os.WriteFile(filepath.Join(root, "mall", "catalog.yaml"), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			after, ok := Load(root).Get("mall")
			if !ok || after.Name != before.Name || after.Description != before.Description || after.ArtifactDigest != before.ArtifactDigest {
				t.Fatal("invalid presentation must leave the original template intact")
			}
		})
	}
}
