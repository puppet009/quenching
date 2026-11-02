package templates

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// catalog.yaml is presentation only and is outside the runtime artifact digest.
// It cannot configure workloads, enable a template, or certify a fault.
type catalogPresentation struct {
	Name               string                         `yaml:"name"`
	Description        string                         `yaml:"description"`
	BusinessScenarios  []string                       `yaml:"business_scenarios"`
	LearningObjectives []string                       `yaml:"learning_objectives"`
	Overview           []string                       `yaml:"overview"`
	Audience           []string                       `yaml:"audience"`
	Cover              *CatalogCover                  `yaml:"cover"`
	Services           map[string]servicePresentation `yaml:"services"`
}

type servicePresentation struct {
	DisplayName string `yaml:"display_name"`
	Description string `yaml:"description"`
}

func loadCatalogPresentation(m *Manifest, dir string) error {
	path := filepath.Join(dir, "catalog.yaml")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("catalog.yaml must be a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var catalog catalogPresentation
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&catalog); err != nil {
		return err
	}
	if strings.TrimSpace(catalog.Name) == "" || strings.TrimSpace(catalog.Description) == "" {
		return fmt.Errorf("catalog name and description are required")
	}
	known := make(map[string]bool, len(m.Services))
	for _, service := range m.Services {
		known[service.Name] = true
	}
	for name := range catalog.Services {
		if !known[name] {
			return fmt.Errorf("catalog references undeclared service %q", name)
		}
	}
	if err := loadCatalogCover(catalog.Cover, dir, m.Slug); err != nil {
		return err
	}
	m.Name, m.Description = catalog.Name, catalog.Description
	m.BusinessScenarios, m.LearningObjectives = catalog.BusinessScenarios, catalog.LearningObjectives
	m.Overview, m.Audience, m.Cover = catalog.Overview, catalog.Audience, catalog.Cover
	for i := range m.Services {
		presentation := catalog.Services[m.Services[i].Name]
		m.Services[i].DisplayName = presentation.DisplayName
		m.Services[i].Description = presentation.Description
	}
	return nil
}
