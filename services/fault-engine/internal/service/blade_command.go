// blade_command.go ChaosBlade 命令构造（纯函数）。
// 设计：把"注入规格 -> blade 命令参数"做成纯函数，不接触真实环境，
// 可在无集群机器上完整单测；真正执行交给 BladeExecutor 接口（云上接 blade，测试用 fake）。
//
// ChaosBlade 在集群里以 daemonset 方式运行（agent），平台对目标 Pod 用 kubectl exec
// 调用 blade，或通过 agent HTTP 接口。这里先产出标准 CLI 参数，执行通道在 blade_cloudexec.go。

package service

import (
	"fmt"

	"github.com/quenching/fault-engine/internal/model"
)

// 注入/销毁/查询子命令。
const (
	cmdCreate  = "create"
	cmdDestroy = "destroy"
	cmdStatus  = "status"
)

// BuildBladeCreateArgs 根据注入规格构造 `blade create <scope> <action> [flags]` 参数。
// 返回 args（不含 "blade" 本身，由执行通道拼接）。
// 纯函数：同样输入必然同样输出，便于校验和审计。
func BuildBladeCreateArgs(spec model.InjectSpec) ([]string, error) {
	if spec.Scope == "pod" {
		return BuildPodBladeArgs(spec)
	}
	switch spec.Kind {
	case model.KindCPU:
		percent := defaultInt(spec.CPUPercent, 100)
		if err := validatePercent(percent); err != nil {
			return nil, err
		}
		args := []string{cmdCreate, "cpu", "fullload", "--cpu-percent", itoa(percent)}
		args = appendCommon(args, spec)
		return args, nil

	case model.KindMem:
		percent := defaultInt(spec.MemPercent, 80)
		if err := validatePercent(percent); err != nil {
			return nil, err
		}
		args := []string{cmdCreate, "mem", "load", "--mem-percent", itoa(percent)}
		args = appendCommon(args, spec)
		return args, nil

	case model.KindDisk:
		percent := defaultInt(spec.DiskPercent, 80)
		if err := validatePercent(percent); err != nil {
			return nil, err
		}
		// fill 动作填充磁盘；--path 指定目录（容器内默认 /）。
		args := []string{cmdCreate, "disk", "fill", "--percent", itoa(percent), "--path", "/"}
		args = appendCommon(args, spec)
		return args, nil

	case model.KindNetDelay:
		ms := defaultInt(spec.DelayMillis, 500)
		args := []string{cmdCreate, "network", "delay",
			"--time", itoa(ms),
			"--interface", defaultStr(spec.NetworkInterface, "eth0"),
			"--timeout", durationFlag(spec)}
		args = appendCommon(args, spec)
		return args, nil

	case model.KindNetLoss:
		percent := defaultInt(spec.LossPercent, 50)
		if err := validatePercent(percent); err != nil {
			return nil, err
		}
		args := []string{cmdCreate, "network", "loss",
			"--percent", itoa(percent),
			"--interface", defaultStr(spec.NetworkInterface, "eth0"),
			"--timeout", durationFlag(spec)}
		args = appendCommon(args, spec)
		return args, nil

	case model.KindProcess:
		if spec.TargetProcess == "" {
			return nil, fmt.Errorf("进程类故障必须指定 target_process")
		}
		args := []string{cmdCreate, "process", "kill", "--process", spec.TargetProcess}
		args = appendCommon(args, spec)
		return args, nil

	case model.KindThreadpool:
		// 预留：JVM 线程池打满通常走 jvm 插件/自定义脚本，MVP 后期补。
		return nil, fmt.Errorf("线程池故障暂未支持（预留）")

	default:
		return nil, fmt.Errorf("不支持的故障类型: %s", spec.Kind)
	}
}

// BuildBladeDestroyArgs 构造恢复命令：blade destroy <experimentUID>。
func BuildBladeDestroyArgs(uid string) ([]string, error) {
	if uid == "" {
		return nil, fmt.Errorf("恢复需要 experiment uid")
	}
	return []string{cmdDestroy, uid}, nil
}

// BuildBladeStatusArgs 查询实验状态：blade status <experimentUID>。
func BuildBladeStatusArgs(uid string) ([]string, error) {
	if uid == "" {
		return nil, fmt.Errorf("查询状态需要 experiment uid")
	}
	return []string{cmdStatus, uid, "--type", "command"}, nil
}

// appendCommon 追加公共参数。
// 平台默认掌控恢复时机，故不设自动超时（duration=0）；需要自动恢复时由 spec 传入。
func appendCommon(args []string, spec model.InjectSpec) []string {
	if spec.DurationSec > 0 {
		args = append(args, "--timeout", itoa(spec.DurationSec))
	}
	// ChaosBlade 1.x 对 k8s 场景常用 --namespace/--pod/--container，
	// 若用 blade create k8s ... 子命令则参数不同；执行通道负责把这些目标映射为
	// kubectl exec 的定位参数，这里仅保留进程匹配（如需要）。
	if spec.TargetProcess != "" {
		args = append(args, "--process", spec.TargetProcess)
	}
	return args
}

func durationFlag(spec model.InjectSpec) string {
	return itoa(defaultInt(spec.DurationSec, 0))
}

func validatePercent(p int) error {
	if p < 0 || p > 100 {
		return fmt.Errorf("百分比必须在 0-100 之间，实际 %d", p)
	}
	return nil
}

func defaultInt(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}

func defaultStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
