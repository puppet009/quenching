package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest 在 root 下创建 <slug>/template.yaml，并额外创建 helm 目录
// 供需要合法 chart 的用例使用；清单内 chart 指向其他路径的用例不受影响。
func writeManifest(t *testing.T, root, slug, content string) {
	t.Helper()
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(filepath.Join(dir, "helm"), 0o755); err != nil {
		t.Fatalf("建 chart 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(content), 0o644); err != nil {
		t.Fatalf("写清单失败: %v", err)
	}
}

const validManifest = `apiVersion: quenching/v1
slug: mall
name: 电商交易系统
domain: ecommerce
description: 三服务电商靶场
difficulty: medium
tech_stack: [Go, MySQL]
chart: helm
sort_order: 100
services:
  - name: user-service
    port: 8080
  - name: order-service
    workload: order-svc
    role: middleware
`

func TestLoad_ValidManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "mall", validManifest)

	reg := Load(root)
	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("应加载 1 个模板，got %d", len(list))
	}
	m := list[0]
	if m.Slug != "mall" || m.Name != "电商交易系统" || m.Chart != "helm" {
		t.Errorf("核心字段不匹配: %+v", m)
	}
	if len(m.Services) != 2 || m.Services[0].Workload != "user-service" || m.Services[0].Role != RoleBusiness {
		t.Errorf("缺省填充不正确: %+v", m.Services)
	}
	if m.Services[1].Workload != "order-svc" || m.Services[1].Role != RoleMiddleware {
		t.Errorf("显式字段不正确: %+v", m.Services[1])
	}

	got, ok := reg.Get("mall")
	if !ok || got.Slug != "mall" {
		t.Errorf("Get 已启用模板应命中, ok=%v", ok)
	}
	if path := reg.ChartPath(got); path != filepath.Join(root, "mall", "helm") {
		t.Errorf("ChartPath 不匹配: %s", path)
	}
}

func TestLoad_SortedByOrderThenSlug(t *testing.T) {
	root := t.TempDir()
	custom := func(slug, order string) string {
		return `apiVersion: quenching/v1
slug: ` + slug + `
name: ` + slug + `
chart: helm
sort_order: ` + order + `
services:
  - name: svc-a
`
	}
	writeManifest(t, root, "beta", custom("beta", "50"))
	writeManifest(t, root, "alpha", custom("alpha", "100"))
	writeManifest(t, root, "gamma", custom("gamma", "50"))

	list := Load(root).List()
	if len(list) != 3 {
		t.Fatalf("应加载 3 个模板，got %d", len(list))
	}
	got := []string{list[0].Slug, list[1].Slug, list[2].Slug}
	want := []string{"beta", "gamma", "alpha"} // 50 内按 slug，100 在后
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("排序不匹配，got %v want %v", got, want)
		}
	}
}

func TestLoad_DisabledHidden(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "off", `apiVersion: quenching/v1
slug: off
name: 停用模板
chart: helm
enabled: false
services:
  - name: svc-a
`)
	reg := Load(root)
	if len(reg.List()) != 0 {
		t.Error("enabled=false 不应出现在列表")
	}
	if _, ok := reg.Get("off"); ok {
		t.Error("enabled=false 时 Get 应报不存在")
	}
}

func TestLoad_BadManifestSkipped(t *testing.T) {
	cases := map[string]string{
		"bad-api-version": "apiVersion: other/v1\nslug: badapi\nname: x\nchart: helm\nservices:\n  - name: a\n",
		"bad-slug":        "apiVersion: quenching/v1\nslug: Bad_Slug\nname: x\nchart: helm\nservices:\n  - name: a\n",
		"empty-name":      "apiVersion: quenching/v1\nslug: noname\nname: ''\nchart: helm\nservices:\n  - name: a\n",
		"missing-chart":   "apiVersion: quenching/v1\nslug: nochart\nname: x\nchart: nope\nservices:\n  - name: a\n",
		"no-services":     "apiVersion: quenching/v1\nslug: nosvc\nname: x\nchart: helm\nservices: []\n",
		"dup-service":     "apiVersion: quenching/v1\nslug: dupsvc\nname: x\nchart: helm\nservices:\n  - name: a\n  - name: a\n",
		"bad-difficulty":  "apiVersion: quenching/v1\nslug: baddiff\nname: x\nchart: helm\ndifficulty: expert\nservices:\n  - name: a\n",
		"bad-image":       "apiVersion: quenching/v1\nslug: badimg\nname: x\nchart: helm\nservices:\n  - name: a\nimages:\n  - name: b\n    context: .\n    dockerfile: Dockerfile\n",
		"chart-escape":    "apiVersion: quenching/v1\nslug: escape\nname: x\nchart: ../../etc\nservices:\n  - name: a\n",
	}
	for slug, body := range cases {
		root := t.TempDir()
		writeManifest(t, root, slug, body)
		reg := Load(root)
		if len(reg.List()) != 0 {
			t.Errorf("用例 %s：坏模板应被跳过，实际加载了 %d 个", slug, len(reg.List()))
		}
	}
}

func TestLoad_DuplicateSlugKeepsFirst(t *testing.T) {
	root := t.TempDir()
	mk := func(dir, slug string) {
		writeManifest(t, root, dir, `apiVersion: quenching/v1
slug: `+slug+`
name: `+dir+`
chart: helm
services:
  - name: a
`)
	}
	mk("first-dir", "same")
	mk("second-dir", "same")

	reg := Load(root)
	if len(reg.List()) != 1 {
		t.Fatalf("重复 slug 应只保留 1 个，got %d", len(reg.List()))
	}
	if reg.List()[0].Name != "first-dir" {
		t.Errorf("应保留目录序在先的 first-dir，got %s", reg.List()[0].Name)
	}
}

func TestLoad_MissingAndEmptyRoot(t *testing.T) {
	reg := Load(filepath.Join(t.TempDir(), "not-exist"))
	if len(reg.List()) != 0 {
		t.Error("根目录不存在时应返回空注册表")
	}
	empty := Load(t.TempDir())
	if len(empty.List()) != 0 {
		t.Error("空目录应返回空注册表")
	}
	if _, ok := empty.Get("any"); ok {
		t.Error("空注册表 Get 不应命中")
	}
}

func TestSlugPattern(t *testing.T) {
	valid := []string{"a1", "mall", "a-b", "abc-123", "a" + strings.Repeat("b", 38) + "z"} // 40 字符
	for _, s := range valid {
		if !slugPattern.MatchString(s) {
			t.Errorf("slug %q 应合法（len=%d）", s, len(s))
		}
	}
	invalid := []string{"", "-ab", "ab-", "a b", "UPPER", "a_b", strings.Repeat("a", 41)}
	for _, s := range invalid {
		if slugPattern.MatchString(s) {
			t.Errorf("slug %q 应非法（len=%d）", s, len(s))
		}
	}
}

func TestEmptyRegistry(t *testing.T) {
	reg := EmptyRegistry()
	if reg.List() != nil {
		t.Error("EmptyRegistry.List 应为 nil")
	}
	if _, ok := reg.Get("x"); ok {
		t.Error("EmptyRegistry.Get 不应命中")
	}
}
