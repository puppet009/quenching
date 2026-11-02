package service

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTerminalRoleHasNoGlobalExecBinding(t *testing.T) {
	bin := os.Getenv("HELM_BIN")
	if bin == "" {
		t.Skip("HELM_BIN required for deployment rendering")
	}
	for _, faultEnabled := range []string{"true", "false"} {
		out, err := exec.Command(bin, "template", "quenching", "../../../../deploy/helm", "--namespace", "platform", "--set", "fullnameOverride=access-qa", "--set", "faultExecution.enabled="+faultEnabled).CombinedOutput()
		if err != nil {
			t.Fatalf("render: %v %s", err, out)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(out))
		foundRole, foundBind, foundConfig := false, false, false
		for {
			var doc struct {
				Kind     string
				Metadata struct{ Name string }
				Rules    []struct {
					Resources, Verbs []string
					ResourceNames    []string `yaml:"resourceNames"`
				}
				RoleRef struct{ Name string } `yaml:"roleRef"`
				Data    map[string]string
			}
			err := decoder.Decode(&doc)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if doc.Kind == "ClusterRoleBinding" && doc.RoleRef.Name == "access-qa-sandbox-terminal" {
				t.Fatal("terminal exec role is globally bound")
			}
			if doc.Kind == "ConfigMap" {
				for _, v := range doc.Data {
					if strings.Contains(v, `terminal_cluster_role: "access-qa-sandbox-terminal"`) {
						foundConfig = true
					}
				}
			}
			if doc.Kind != "ClusterRole" {
				continue
			}
			for _, rule := range doc.Rules {
				if len(rule.Resources) == 1 && rule.Resources[0] == "pods/exec" {
					if doc.Metadata.Name != "access-qa-sandbox-terminal" || len(rule.Verbs) != 1 || rule.Verbs[0] != "create" {
						t.Fatal("exec outside unbound template", doc.Metadata.Name, rule)
					}
					foundRole = true
				}
				for _, verb := range rule.Verbs {
					if verb == "bind" {
						if len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != "access-qa-sandbox-terminal" || len(rule.Resources) != 1 || rule.Resources[0] != "clusterroles" {
							t.Fatal("unrestricted bind delegation", rule)
						}
						foundBind = true
					}
					if verb == "escalate" || verb == "*" {
						t.Fatal("unrestricted escalation", doc.Metadata.Name)
					}
				}
			}
		}
		if !foundRole || !foundBind || !foundConfig {
			t.Fatal("missing runtime access wiring", foundRole, foundBind, foundConfig)
		}
	}
}
