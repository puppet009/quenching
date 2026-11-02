package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestServerTargetsExposeActualContainerWorkspacePaths(t *testing.T) {
	pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "frontend-one", Namespace: "sandbox-9", Annotations: map[string]string{
		"quenching.io/terminal-directories": `{"web":{"working_dir":"/workspace/mall-app-web","deployment_dir":"/usr/share/nginx/html","config_dir":"/etc/nginx/conf.d"}}`,
	}}, Spec: core.PodSpec{Containers: []core.Container{{Name: "web", WorkingDir: "/"}, {Name: "sidecar", WorkingDir: "/var/log"}}}}
	targets, err := NewK8sClient(fake.NewSimpleClientset(pod)).ListTargets(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	// Decode the public wire contract independently of Go field names.
	var wire []map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	containers := wire[0]["containers"].([]any)
	web := containers[0].(map[string]any)
	if web["working_dir"] != "/workspace/mall-app-web" || web["deployment_dir"] != "/usr/share/nginx/html" || web["config_dir"] != "/etc/nginx/conf.d" {
		t.Fatalf("workspace missing from target: %s", raw)
	}
	if containers[1].(map[string]any)["working_dir"] != "/var/log" {
		t.Fatalf("container-specific working directory lost: %s", raw)
	}
}

func TestTerminalStartsInRequestedDirectoryAndReportsDirectoryChanges(t *testing.T) {
	bin := os.Getenv("TERMINAL_TEST_BASH")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("bash")
		if err != nil {
			t.Skip("bash required for interactive shell regression")
		}
	}
	dir := filepath.Join(t.TempDir(), "mall space%")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "terminal.sh")
	if err := os.WriteFile(script, []byte(terminalCommand[2]), 0600); err != nil {
		t.Fatal(err)
	}
	// Execute the exact script from a file: Windows/MSYS rewrites backslashes
	// inside a -c argument, while Kubernetes passes argv to a native Linux sh.
	command := exec.Command(bin, "--noprofile", "--norc", filepath.ToSlash(script), filepath.ToSlash(dir))
	command.Stdin = strings.NewReader("printf 'QDIR=%s\\n' \"$PWD\"\nmkdir -p child\ncd child\nprintf 'QAFTER=%s\\n' \"$PWD\"\nexit\n")
	command.Env = append(os.Environ(), "MSYS=winsymlinks:nativestrict", "HOME="+filepath.ToSlash(t.TempDir()))
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("shell failed: %v %s", err, out)
	}
	if !strings.Contains(string(out), "mall space%\n") || !strings.Contains(string(out), "mall space%/child\n") {
		t.Fatalf("shell did not start in requested project: %s", out)
	}
	if !strings.Contains(string(out), "\x1b]7;file://localhost/") || !strings.Contains(string(out), "mall%20space%25/child\a") {
		t.Fatalf("shell did not report actual cd using OSC7: %q", out)
	}
}

func TestTerminalDirectoryMetadataRejectsUnsafePaths(t *testing.T) {
	for _, dir := range []string{"relative", "/tmp/../app", "//app", "/app\nother", "/app\x00other"} {
		t.Run(dir, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]map[string]string{"web": {"working_dir": dir}})
			pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web", Namespace: "sandbox-9", Annotations: map[string]string{terminalDirectoriesAnnotation: string(raw)}}, Spec: core.PodSpec{Containers: []core.Container{{Name: "web"}}}}
			if _, err := NewK8sClient(fake.NewSimpleClientset(pod)).ListTargets(context.Background(), 9); err == nil {
				t.Fatal("accepted invalid template directory")
			}
		})
	}
}

func TestTerminalInitFileIsPortableForShFallback(t *testing.T) {
	bin := os.Getenv("TERMINAL_TEST_SH")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("dash")
		if err != nil {
			t.Skip("dash required for strict POSIX shell regression")
		}
	}
	rc := strings.SplitN(strings.SplitN(terminalCommand[2], "<<'QUENCHING_RC'\n", 2)[1], "\nQUENCHING_RC", 2)[0]
	command := exec.Command(bin, "-n")
	command.Stdin = strings.NewReader(rc)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fallback shell cannot parse terminal init: %v %s", err, out)
	}
}

func TestTerminalCwdEncodingPreservesLiteralBackslash(t *testing.T) {
	bin := os.Getenv("TERMINAL_TEST_BASH")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("bash")
		if err != nil {
			t.Skip("bash required")
		}
	}
	rc := strings.SplitN(strings.SplitN(terminalCommand[2], "<<'QUENCHING_RC'\n", 2)[1], "\nQUENCHING_RC", 2)[0]
	script := filepath.Join(t.TempDir(), "cwd.sh")
	if err := os.WriteFile(script, []byte(rc+"\nPWD='/tmp/back\\slash'; __quenching_report_cwd"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(bin, "--noprofile", "--norc", filepath.ToSlash(script))
	command.Env = append(os.Environ(), "HOME="+filepath.ToSlash(t.TempDir()), "QUENCHING_TERMINAL_DIR="+filepath.ToSlash(t.TempDir()))
	if out, err := command.CombinedOutput(); err != nil || !strings.Contains(string(out), "/tmp/back%5Cslash\a") {
		t.Fatalf("cwd URI would navigate to the wrong directory: %v %q", err, out)
	}
}
