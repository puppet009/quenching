package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
)

// TestBuildCreateArgs 验证各故障类型构造出的 blade 命令参数正确。
func TestBuildCreateArgs(t *testing.T) {
	cases := []struct {
		name      string
		spec      model.InjectSpec
		wantParts []string // 期望包含的关键参数
	}{
		{"CPU满载", model.InjectSpec{Kind: model.KindCPU, CPUPercent: 90},
			[]string{"create", "cpu", "fullload", "--cpu-percent", "90"}},
		{"CPU默认100", model.InjectSpec{Kind: model.KindCPU},
			[]string{"cpu", "fullload", "100"}},
		{"内存占用", model.InjectSpec{Kind: model.KindMem, MemPercent: 70},
			[]string{"mem", "load", "--mem-percent", "70"}},
		{"磁盘填充", model.InjectSpec{Kind: model.KindDisk, DiskPercent: 50},
			[]string{"disk", "fill", "--percent", "50", "--path", "/"}},
		{"网络延迟", model.InjectSpec{Kind: model.KindNetDelay, DelayMillis: 800},
			[]string{"network", "delay", "--time", "800", "--interface", "eth0"}},
		{"网络丢包", model.InjectSpec{Kind: model.KindNetLoss, LossPercent: 30},
			[]string{"network", "loss", "--percent", "30"}},
		{"进程kill", model.InjectSpec{Kind: model.KindProcess, TargetProcess: "nginx"},
			[]string{"process", "kill", "--process", "nginx"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args, err := service.BuildBladeCreateArgs(c.spec)
			if err != nil {
				t.Fatalf("构造失败: %v", err)
			}
			joined := strings.Join(args, " ")
			for _, p := range c.wantParts {
				if !strings.Contains(joined, p) {
					t.Errorf("命令缺少 %q，实际: %s", p, joined)
				}
			}
		})
	}
}

// TestBuildCreateArgsValidation 验证参数校验。
func TestBuildCreateArgsValidation(t *testing.T) {
	if _, err := service.BuildBladeCreateArgs(model.InjectSpec{Kind: model.KindCPU, CPUPercent: 150}); err == nil {
		t.Error("CPU 150% 应报错")
	}
	if _, err := service.BuildBladeCreateArgs(model.InjectSpec{Kind: model.KindProcess}); err == nil {
		t.Error("进程类未指定 target_process 应报错")
	}
	if _, err := service.BuildBladeCreateArgs(model.InjectSpec{Kind: "unknown"}); err == nil {
		t.Error("未知故障类型应报错")
	}
}

// TestBuildDestroyArgs 验证恢复命令必须带 uid。
func TestBuildDestroyArgs(t *testing.T) {
	if _, err := service.BuildBladeDestroyArgs(""); err == nil {
		t.Error("空 uid 恢复应报错")
	}
	args, err := service.BuildBladeDestroyArgs("uid-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "destroy" || args[1] != "uid-123" {
		t.Errorf("恢复命令错误: %v", args)
	}
}

// TestInjectAndRevokeFlow 用 fakeExecutor 验证注入->拿uid->恢复全流程。
func TestInjectAndRevokeFlow(t *testing.T) {
	exec := &fakeExecutor{}
	inj := service.NewInjector(exec)
	ctx := context.Background()
	spec := model.InjectSpec{
		Kind: model.KindCPU, CPUPercent: 100,
		TargetNamespace: "sandbox-1", TargetPod: "order-svc",
	}

	exp, err := inj.Inject(ctx, spec)
	if err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if exp.ExperimentUID != "fake-uid-1" {
		t.Errorf("uid 错误: %s", exp.ExperimentUID)
	}
	if exp.Status != model.StatusRunning {
		t.Errorf("状态应为 running，实际 %s", exp.Status)
	}
	if !strings.Contains(exp.Command, "cpu fullload") {
		t.Errorf("审计命令错误: %s", exp.Command)
	}

	// 恢复
	if err := inj.Revoke(ctx, spec, exp.ExperimentUID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	last := exec.lastArgs()
	if len(last) < 2 || last[0] != "destroy" || last[1] != "fake-uid-1" {
		t.Errorf("恢复未用正确 uid，实际: %v", last)
	}
}

// TestParseUID 验证 blade JSON 输出解析。
func TestParseUID(t *testing.T) {
	uid, err := service.ParseBladeUID(`{"code":200,"success":true,"result":"abc123"}`)
	if err != nil || uid != "abc123" {
		t.Errorf("解析成功响应失败: uid=%s err=%v", uid, err)
	}
	if _, err := service.ParseBladeUID(`{"code":500,"success":false,"error":"boom"}`); err == nil {
		t.Error("失败响应应返回错误")
	}
}
