package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRejectForeignServiceImportsIncludingTestsAndPublicPackages(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "services/first/go.mod", "module github.com/quenching/first\n")
	fixtureFile(t, root, "services/second/go.mod", "module github.com/quenching/second\n")
	fixtureFile(t, root, "services/first/internal/example_test.go", `package internal
import other "github.com/quenching/second/public"
`)
	errors := boundaryErrors(root)
	if len(errors) != 1 || !strings.Contains(errors[0], "github.com/quenching/second/public") {
		t.Fatalf("expected cross-service test import rejection, got %v", errors)
	}
}

func TestAllowOwnServiceSharedModulesAndCommentedImports(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "services/first/go.mod", "module github.com/quenching/first\n")
	fixtureFile(t, root, "services/second/go.mod", "module github.com/quenching/second\n")
	fixtureFile(t, root, "services/first/main.go", `package main
// import "github.com/quenching/second/internal/private"
import (
    "github.com/quenching/first/internal/own"
    "github.com/quenching/go-common/logger"
    "github.com/quenching/proto/contract"
)
`)
	if errors := boundaryErrors(root); len(errors) != 0 {
		t.Fatalf("allowed imports rejected: %v", errors)
	}
}

func TestSharedModuleCannotDependOnService(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "services/first/go.mod", "module github.com/quenching/first\n")
	fixtureFile(t, root, "packages/proto/generated.pb.go", `package proto
import "github.com/quenching/first/public"
`)
	if errors := boundaryErrors(root); len(errors) != 1 {
		t.Fatalf("expected generated shared import rejection, got %v", errors)
	}
}

func TestUnreadableOrInvalidSourceFailsClosed(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "services/first/go.mod", "module github.com/quenching/first\n")
	fixtureFile(t, root, "services/first/bad.go", "not Go source")
	if errors := boundaryErrors(root); len(errors) == 0 {
		t.Fatal("invalid Go source passed")
	}
}
