package service

import (
	"encoding/json"
	"fmt"
)

func ValidateCatalogConfiguration(raw json.RawMessage, podRequired bool) error {
	scenario, err := ParseScenario(raw)
	if err != nil {
		return err
	}
	if !podRequired {
		return nil
	}
	if !autoInjectable[scenario.Kind] {
		return fmt.Errorf("当前执行器不支持此类故障")
	}
	if scenario.Spec.Scope != "pod" {
		return fmt.Errorf("模板故障缺少 Pod 隔离配置")
	}
	spec := scenario.Spec
	spec.Kind = scenario.Kind
	spec.TargetNamespace = "sandbox-1"
	spec.TargetPod = "validation-target"
	_, err = BuildPodBladeArgs(spec)
	return err
}
