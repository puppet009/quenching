package templates

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCatalogCoverFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeManifest(t, root, "mall", validManifest)
	media := filepath.Join(root, "mall", "catalog-assets")
	if err := os.MkdirAll(media, 0755); err != nil {
		t.Fatal(err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aV1kAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "home.png"), png, 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCatalogCoverAndDetailedIntroductionPreserveRuntimeIdentity(t *testing.T) {
	root := writeCatalogCoverFixture(t)
	before, _ := Load(root).Get("mall")
	content := "name: 商城实训\ndescription: 交易系统排障训练\noverview: [围绕真实业务理解应用与中间件依赖]\naudience: [掌握基础开发的工程师]\ncover:\n  file: home.png\n  alt: 商城首页\n"
	if err := os.WriteFile(filepath.Join(root, "mall", "catalog.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	after, ok := Load(root).Get("mall")
	if !ok || after.Name != "商城实训" {
		t.Fatal("cover presentation was not loaded")
	}
	if after.ArtifactDigest != before.ArtifactDigest {
		t.Fatal("a display image changed the runtime identity")
	}
	raw, _ := json.Marshal(after)
	for _, value := range []string{"/api/v1/template-media/mall/home.png?v=", "商城首页", "overview", "audience"} {
		if !strings.Contains(string(raw), value) {
			t.Fatalf("public presentation is missing %q", value)
		}
	}
	if strings.Contains(string(raw), root) || strings.Contains(string(raw), "catalog-assets") {
		t.Fatal("API leaked an internal filesystem path")
	}
}

func TestCatalogCoverRejectsTraversalAndNonImages(t *testing.T) {
	for _, file := range []string{"../template.yaml", "home.svg", "missing.png", "disguised.png"} {
		t.Run(file, func(t *testing.T) {
			root := writeCatalogCoverFixture(t)
			before, _ := Load(root).Get("mall")
			if err := os.WriteFile(filepath.Join(root, "mall", "catalog-assets", "disguised.png"), []byte("<html>not an image</html>"), 0644); err != nil {
				t.Fatal(err)
			}
			content := "name: 新展示\ndescription: 新简介\ncover:\n  file: " + file + "\n  alt: 首页\n"
			if err := os.WriteFile(filepath.Join(root, "mall", "catalog.yaml"), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			after, ok := Load(root).Get("mall")
			if !ok || after.Name != before.Name || after.ArtifactDigest != before.ArtifactDigest {
				t.Fatal("invalid cover must preserve the original template")
			}
		})
	}
}
