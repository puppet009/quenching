package service

import (
	"bytes"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPlatformResourcesDeclared(t *testing.T) {
	bin := os.Getenv("HELM_BIN")
	if bin == "" {
		t.Skip("HELM_BIN required for actual deployment rendering")
	}
	out, e := exec.Command(bin, "template", "quenching", "../../../../deploy/helm", "--namespace", "platform", "-f", "../../../../deploy/server/values-server.yaml").CombinedOutput()
	if e != nil {
		t.Fatalf("render: %v %s", e, out)
	}
	dec := yaml.NewDecoder(bytes.NewReader(out))
	count := 0
	for {
		var v struct {
			Kind     string
			Metadata struct{ Name string }
			Spec     struct {
				Template struct {
					Spec struct {
						PriorityClass string `yaml:"priorityClassName"`
						Containers    []struct {
							Resources struct{ Requests, Limits map[string]string }
						}
					}
				}
			}
		}
		e = dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if v.Kind != "Deployment" {
			continue
		}
		count++
		if v.Spec.Template.Spec.PriorityClass != "quenching-platform" {
			t.Fatalf("priority missing: %s", v.Metadata.Name)
		}
		for _, c := range v.Spec.Template.Spec.Containers {
			if c.Resources.Requests["cpu"] == "" || c.Resources.Requests["memory"] == "" || c.Resources.Limits["cpu"] == "" || c.Resources.Limits["memory"] == "" {
				t.Fatalf("resources missing: %s", v.Metadata.Name)
			}
		}
	}
	if count != 8 {
		t.Fatal(count)
	}
}

func TestPriorityPrerequisitesForFreshInstall(t *testing.T) {
	bin := os.Getenv("HELM_BIN")
	if bin == "" {
		t.Skip("HELM_BIN required")
	}
	out, e := exec.Command(bin, "template", "quenching", "../../../../deploy/helm", "--namespace", "platform", "--show-only", "templates/resource-priorities.yaml").CombinedOutput()
	if e != nil {
		t.Fatal(e, string(out))
	}
	for _, v := range []string{"meta.helm.sh/release-name: quenching", "meta.helm.sh/release-namespace: platform", "app.kubernetes.io/managed-by: Helm"} {
		if !strings.Contains(string(out), v) {
			t.Fatal("preflight cannot be adopted by Helm:", v)
		}
	}
	doc, e := os.ReadFile("../../../../deploy/server/README.md")
	if e != nil {
		t.Fatal(e)
	}
	pre := strings.Index(string(doc), "--show-only templates/resource-priorities.yaml")
	middleware := strings.Index(string(doc), "kubectl apply -f deploy/server/manifests/10-middleware.yaml")
	if pre < 0 || pre > middleware {
		t.Fatal("PriorityClass prerequisite is after middleware")
	}
}
