package templates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateRevisionTracksExecutableAdaptation(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "adaptation"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "adaptation", "adapter.json")
	os.WriteFile(path, []byte("one"), 0644)
	first, err := TemplateArtifactDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("two"), 0644)
	second, err := TemplateArtifactDigest(root)
	if err != nil || first == second {
		t.Fatal("changed executable artifact did not invalidate digest")
	}
}

func TestTemplateRevisionTracksUpstreamSource(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "upstream"), 0755)
	file := filepath.Join(root, "upstream", "main.java")
	os.WriteFile(file, []byte("one"), 0644)
	a, _ := TemplateArtifactDigest(root)
	os.WriteFile(file, []byte("two"), 0644)
	b, _ := TemplateArtifactDigest(root)
	if a == b {
		t.Fatal("source changed without revision change")
	}
}
