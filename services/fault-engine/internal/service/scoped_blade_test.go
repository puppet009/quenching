package service

import (
	"github.com/quenching/fault-engine/internal/model"
	"strings"
	"testing"
)

func TestTemplateBladeTargetsPod(t *testing.T) {
	args, err := BuildBladeCreateArgs(model.InjectSpec{Kind: model.KindCPU, Scope: "pod", TargetNamespace: "sandbox-9", TargetPod: "mall-portal-abc", CPUPercent: 50, DurationSec: 60})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "create k8s pod-cpu") || !strings.Contains(joined, "--names mall-portal-abc") || !strings.Contains(joined, "--namespace sandbox-9") {
		t.Fatalf("unscoped command: %s", joined)
	}
	_, err = BuildBladeCreateArgs(model.InjectSpec{Kind: model.KindCPU, Scope: "pod", TargetNamespace: "platform", TargetPod: "api-gateway"})
	if err == nil {
		t.Fatal("platform scope accepted")
	}
}
