// Check direct Go imports without depending on a linter's compiler version.
// Every Go file, including tests and generated code, is parsed with Go's AST.
// Services may import themselves and shared modules, never another service;
// shared modules may not import service implementations.
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func boundaryErrors(root string) []string {
	var errors []string
	modules := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		return []string{err.Error()}
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "services", entry.Name(), "go.mod"))
		if os.IsNotExist(err) {
			continue // Python service has no Go module.
		}
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" {
				modules[entry.Name()] = strings.Trim(fields[1], "\"")
				break
			}
		}
		if modules[entry.Name()] == "" {
			errors = append(errors, "missing module directive: "+entry.Name())
		}
	}
	if len(modules) == 0 {
		errors = append(errors, "no Go service modules found")
	}
	for _, area := range []string{"services", "packages"} {
		areaPath := filepath.Join(root, area)
		if _, err := os.Stat(areaPath); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(areaPath, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "vendor" || entry.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, err := filepath.Rel(areaPath, path)
			if err != nil {
				return err
			}
			owner := strings.Split(filepath.ToSlash(rel), "/")[0]
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				errors = append(errors, err.Error())
				return nil
			}
			for _, spec := range file.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", path, err))
					continue
				}
				for service, module := range modules {
					if area == "services" && service == owner {
						continue
					}
					if importPath == module || strings.HasPrefix(importPath, module+"/") {
						errors = append(errors, fmt.Sprintf("%s: forbidden cross-service import %s", path, importPath))
					}
				}
			}
			return nil
		})
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	return errors
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	errors := boundaryErrors(*root)
	for _, message := range errors {
		fmt.Fprintln(os.Stderr, message)
	}
	if len(errors) != 0 {
		os.Exit(1)
	}
	fmt.Println("PASS: cross-service Go import boundaries (including tests and generated code)")
}
