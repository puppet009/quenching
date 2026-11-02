package templates

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type mallContainer struct {
	Name         string
	Image        string
	VolumeMounts []struct {
		Name      string
		MountPath string `yaml:"mountPath"`
	} `yaml:"volumeMounts"`
	Env []struct {
		Name      string
		Value     string
		ValueFrom *struct {
			SecretKeyRef *struct {
				Name string
				Key  string
			} `yaml:"secretKeyRef"`
		} `yaml:"valueFrom"`
	}
	Resources struct {
		Limits map[string]string
	}
}

type mallResource struct {
	Kind       string
	Metadata   struct{ Name, Namespace string }
	StringData map[string]string `yaml:"stringData"`
	Spec       struct {
		Selector map[string]any
		Template struct {
			Metadata struct{ Labels, Annotations map[string]string }
			Spec     struct {
				Containers     []mallContainer
				InitContainers []mallContainer `yaml:"initContainers"`
				Volumes        []struct {
					Name                  string
					HostPath              any `yaml:"hostPath"`
					PersistentVolumeClaim *struct {
						ClaimName string `yaml:"claimName"`
					} `yaml:"persistentVolumeClaim"`
				}
			}
		}
	}
}

func TestMallProvidesPersistentSourceAndRuntimeWorkspaces(t *testing.T) {
	resources := renderMallChart(t)
	want := map[string]string{"frontend": "/workspace/mall-app-web", "mall-portal": "/workspace/mall"}
	claims := map[string]bool{}
	for _, item := range resources {
		if item.Kind == "PersistentVolumeClaim" {
			claims[item.Metadata.Name] = true
		}
	}
	for _, item := range resources {
		cwd, ok := want[item.Metadata.Name]
		if item.Kind != "Deployment" || !ok {
			continue
		}
		var dirs map[string]struct {
			WorkingDir    string `json:"working_dir"`
			DeploymentDir string `json:"deployment_dir"`
			ConfigDir     string `json:"config_dir"`
		}
		if err := json.Unmarshal([]byte(item.Spec.Template.Metadata.Annotations["quenching.io/terminal-directories"]), &dirs); err != nil {
			t.Fatal("missing template-owned terminal paths", item.Metadata.Name, err)
		}
		if dirs[item.Metadata.Name].WorkingDir != cwd || dirs[item.Metadata.Name].DeploymentDir == "" || dirs[item.Metadata.Name].ConfigDir == "" {
			t.Fatal("source, runtime and config are not discoverable", item.Metadata.Name, dirs)
		}
		mounted, claimed := false, false
		for _, c := range item.Spec.Template.Spec.Containers {
			for _, v := range c.VolumeMounts {
				if v.Name == "workspace" && v.MountPath == "/workspace" {
					mounted = true
				}
			}
		}
		for _, v := range item.Spec.Template.Spec.Volumes {
			if v.Name == "workspace" && v.PersistentVolumeClaim != nil && claims[v.PersistentVolumeClaim.ClaimName] {
				claimed = true
			}
		}
		if !mounted || !claimed {
			t.Fatal("source/build edits would be lost on container restart", item.Metadata.Name)
		}
		delete(want, item.Metadata.Name)
	}
	if len(want) != 0 {
		t.Fatal("missing editable service", want)
	}
}

func TestMallPortalReceivesConfiguredPaymentSigningKey(t *testing.T) {
	resources := renderMallChart(t)
	secrets := map[string]map[string]string{}
	var portal *mallContainer
	for _, resource := range resources {
		if resource.Kind == "Secret" {
			secrets[resource.Metadata.Name] = resource.StringData
		}
		if resource.Kind == "Deployment" && resource.Metadata.Name == "mall-portal" {
			for _, container := range resource.Spec.Template.Spec.Containers {
				if container.Name == "mall-portal" {
					copy := container
					portal = &copy
				}
			}
		}
	}
	if portal == nil {
		t.Fatal("mall-portal container is missing")
	}
	for _, env := range portal.Env {
		if env.Name != "SANDBOX_PAYMENT_SECRET" {
			continue
		}
		if env.Value != "" || env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
			t.Fatal("sandbox payment key must come from the sandbox Secret")
		}
		ref := env.ValueFrom.SecretKeyRef
		if ref.Name != "mall-credentials" || ref.Key != "sandbox-payment-key" || len(secrets[ref.Name][ref.Key]) < 32 {
			t.Fatal("portal payment key must reference the generated sandbox signing key")
		}
		return
	}
	t.Fatal("application-sandbox.yml requires SANDBOX_PAYMENT_SECRET, but the portal container does not receive it")
}

func renderMallChart(t *testing.T, extra ...string) []mallResource {
	t.Helper()
	helm := os.Getenv("HELM_BIN")
	if helm == "" {
		var err error
		helm, err = exec.LookPath("helm")
		if err != nil {
			t.Skip("set HELM_BIN to run the offline mall chart checks")
		}
	}
	args := append([]string{"template", "mall", filepath.Join("..", "..", "..", "templates", "mall", "helm"), "--namespace", "sandbox-validation"}, extra...)
	out, err := exec.Command(helm, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(out))
	var resources []mallResource
	for {
		var item mallResource
		if err := decoder.Decode(&item); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if item.Kind != "" {
			resources = append(resources, item)
		}
	}
	return resources
}

func cpuMilli(t *testing.T, quantity string) float64 {
	t.Helper()
	multiplier := 1000.0
	if strings.HasSuffix(quantity, "m") {
		quantity, multiplier = strings.TrimSuffix(quantity, "m"), 1
	}
	value, err := strconv.ParseFloat(quantity, 64)
	if err != nil {
		t.Fatalf("invalid CPU limit %q", quantity)
	}
	return value * multiplier
}

func memoryMi(t *testing.T, quantity string) float64 {
	t.Helper()
	multiplier := 1.0
	if strings.HasSuffix(quantity, "Gi") {
		quantity, multiplier = strings.TrimSuffix(quantity, "Gi"), 1024
	} else {
		quantity = strings.TrimSuffix(quantity, "Mi")
	}
	value, err := strconv.ParseFloat(quantity, 64)
	if err != nil {
		t.Fatalf("invalid memory limit %q", quantity)
	}
	return value * multiplier
}

func TestMallChartHasIsolatedWorkloadsWithinQuota(t *testing.T) {
	resources := renderMallChart(t)
	deployments := map[string]mallResource{}
	services := map[string]mallResource{}
	claims := map[string]bool{}
	var totalCPU, totalMemory float64
	for _, item := range resources {
		if item.Metadata.Namespace != "sandbox-validation" {
			t.Fatalf("resource %s is outside the sandbox", item.Metadata.Name)
		}
		switch item.Kind {
		case "Deployment":
			deployments[item.Metadata.Name] = item
			var cpu, memory float64
			for _, container := range item.Spec.Template.Spec.Containers {
				cpu += cpuMilli(t, container.Resources.Limits["cpu"])
				memory += memoryMi(t, container.Resources.Limits["memory"])
			}
			for _, init := range item.Spec.Template.Spec.InitContainers {
				cpu = max(cpu, cpuMilli(t, init.Resources.Limits["cpu"]))
				memory = max(memory, memoryMi(t, init.Resources.Limits["memory"]))
			}
			totalCPU += cpu
			totalMemory += memory
		case "Service":
			services[item.Metadata.Name] = item
		case "PersistentVolumeClaim":
			claims[item.Metadata.Name] = true
		}
	}
	m, ok := Load(filepath.Join("..", "..", "..", "templates")).Get("mall")
	if !ok || len(deployments) != len(m.Services) || len(services) != len(m.Services) {
		t.Fatalf("chart does not deploy declared services: %d deployments, %d services", len(deployments), len(services))
	}
	for _, service := range m.Services {
		dep, exists := deployments[service.Workload]
		svc, svcExists := services[service.Name]
		if !exists || !svcExists {
			t.Fatalf("missing workload or service: %s", service.Name)
		}
		for key, value := range svc.Spec.Selector {
			if dep.Spec.Template.Metadata.Labels[key] != value {
				t.Fatalf("service %s cannot select its workload", service.Name)
			}
		}
		for _, volume := range dep.Spec.Template.Spec.Volumes {
			if volume.HostPath != nil {
				t.Fatal("sandbox must not use hostPath business data")
			}
			if volume.PersistentVolumeClaim != nil && !claims[volume.PersistentVolumeClaim.ClaimName] {
				t.Fatalf("missing claim: %s", volume.PersistentVolumeClaim.ClaimName)
			}
		}
	}
	if len(claims) != 6 || totalCPU > 3000 || totalMemory > 4096 {
		t.Fatalf("invalid storage/quota: claims=%d cpu=%gm memory=%gMi", len(claims), totalCPU, totalMemory)
	}
	t.Logf("effective limits: %gm CPU, %gMi memory", totalCPU, totalMemory)
}

func TestMallChartAllowsImageOverrides(t *testing.T) {
	resources := renderMallChart(t, "--set", "global.image.registry=registry.example.com", "--set", "services.frontend.image.repository=team/frontend", "--set", "services.frontend.image.tag=validation")
	for _, item := range resources {
		if item.Kind == "Deployment" && item.Metadata.Name == "frontend" {
			if got := item.Spec.Template.Spec.Containers[0].Image; got != "registry.example.com/team/frontend:validation" {
				t.Fatalf("image override ignored: %s", got)
			}
			return
		}
	}
	t.Fatal("frontend deployment missing")
}
