// Package service 故障注入编排。
// Injector 负责：注入规格 -> 构造 blade 命令 -> 经 Executor 执行 -> 解析实验 uid；
// 恢复时用 uid 执行 destroy。命令构造是纯函数、执行通道可替换，因此无集群可单测全流程。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/quenching/fault-engine/internal/model"
)

// Injector 故障注入器。
type Injector struct {
	exec  BladeExecutor
	guard AutoscalingGuard
}

func NewInjector(exec BladeExecutor) *Injector {
	return &Injector{exec: exec}
}

// Inject 注入故障，返回带 uid 的实验记录（持久化由调用方/仓储层完成）。
func (in *Injector) Inject(ctx context.Context, spec model.InjectSpec) (*model.Experiment, error) {
	args, err := BuildBladeCreateArgs(spec)
	if err != nil {
		return nil, err
	}
	if resourceFault(spec) && in.guard != nil {
		if spec.ScalingHoldID == "" {
			return nil, errors.New("resource fault freeze identity not persisted")
		}
		if err = in.guard.Acquire(ctx, spec.TargetNamespace, spec.ScalingHoldID, string(spec.Kind)); err != nil {
			return nil, err
		}
	}
	res, err := in.exec.Run(ctx, spec, args)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, errors.New("blade execution not confirmed")
	}
	uid, err := ParseBladeUID(res.Raw)
	if err != nil {
		return nil, err
	}
	return &model.Experiment{
		Kind:          spec.Kind,
		Target:        targetDesc(spec),
		Spec:          spec,
		Status:        model.StatusRunning,
		ExperimentUID: uid,
		Command:       "blade " + strings.Join(args, " "),
	}, nil
}

// Revoke 用实验 uid 恢复故障。
func (in *Injector) Revoke(ctx context.Context, spec model.InjectSpec, uid string) error {
	args, err := BuildBladeDestroyArgs(uid)
	if err != nil {
		return err
	}
	res, err := in.exec.Run(ctx, spec, args)
	if err != nil {
		return err
	}
	var response struct {
		Success bool `json:"success"`
		Code    int  `json:"code"`
	}
	if res == nil || json.Unmarshal([]byte(res.Raw), &response) != nil || !response.Success || response.Code != 200 {
		return errors.New("blade recovery not confirmed")
	}
	if spec.ScalingHoldID != "" && in.guard != nil {
		if err = in.guard.Release(ctx, spec.TargetNamespace, spec.ScalingHoldID); err != nil {
			return err
		}
	}
	return nil
}

func targetDesc(s model.InjectSpec) string {
	t := s.TargetNamespace
	if s.TargetPod != "" {
		t += "/" + s.TargetPod
	}
	return t
}
