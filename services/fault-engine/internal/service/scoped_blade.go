package service

import (
	"fmt"
	"github.com/quenching/fault-engine/internal/model"
	"regexp"
)

var sandboxName = regexp.MustCompile(`^sandbox-[1-9][0-9]*$`)
var podName = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// Kubernetes scenarios require the installed ChaosBlade operator. Never fall back
// to host-level cpu/mem/network experiments when Kubernetes injection fails.
func BuildPodBladeArgs(spec model.InjectSpec) ([]string, error) {
	if !sandboxName.MatchString(spec.TargetNamespace) || !podName.MatchString(spec.TargetPod) {
		return nil, fmt.Errorf("模板故障只能作用于具体沙箱 Pod")
	}
	args := []string{"create", "k8s"}
	switch spec.Kind {
	case model.KindCPU:
		if spec.CPUPercent < 1 || spec.CPUPercent > 80 {
			return nil, fmt.Errorf("模板 CPU 强度须为 1–80")
		}
		args = append(args, "pod-cpu", "fullload", "--cpu-percent", itoa(spec.CPUPercent))
	case model.KindMem:
		if spec.MemPercent < 1 || spec.MemPercent > 80 {
			return nil, fmt.Errorf("模板内存强度须为 1–80")
		}
		args = append(args, "pod-mem", "load", "--mem-percent", itoa(spec.MemPercent), "--mode", "ram")
	case model.KindNetDelay:
		if spec.DelayMillis < 1 || spec.DelayMillis > 3000 {
			return nil, fmt.Errorf("模板延迟须为 1–3000ms")
		}
		args = append(args, "pod-network", "delay", "--time", itoa(spec.DelayMillis), "--interface", "eth0")
	case model.KindNetLoss:
		if spec.LossPercent < 1 || spec.LossPercent > 100 {
			return nil, fmt.Errorf("模板丢包强度非法")
		}
		args = append(args, "pod-network", "loss", "--percent", itoa(spec.LossPercent), "--interface", "eth0")
	default:
		return nil, fmt.Errorf("此故障尚无沙箱隔离适配")
	}
	if spec.DurationSec < 1 || spec.DurationSec > 120 {
		return nil, fmt.Errorf("模板注入须在 1–120 秒内自动撤销")
	}
	return append(args, "--namespace", spec.TargetNamespace, "--names", spec.TargetPod, "--timeout", itoa(spec.DurationSec)), nil
}
