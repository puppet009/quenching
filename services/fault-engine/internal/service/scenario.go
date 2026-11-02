// scenario.go 故障剧本模型与解析。
// 剧本存于 content 域 defects 表（JSONB），fault-engine 运行时只读加载。
// 本文件负责把 JSONB 配置解析为强类型并校验，是"配置化故障"的入口。
package service

import (
	"encoding/json"
	"fmt"

	"github.com/quenching/fault-engine/internal/model"
)

// Scenario 一个故障剧本（defects 表一行的可执行部分）。
type Scenario struct {
	// Code 剧本编码，唯一。
	Code string `json:"code"`
	// Kind 故障类型，对应 blade 支持的类型。
	Kind model.FaultKind `json:"kind"`
	// Title 故障名称。
	Title string `json:"title"`
	// Target 注入目标（namespace 运行时注入，pod/process 配置给出）。
	Target ScenarioTarget `json:"target"`
	// Spec 注入强度参数。
	Spec model.InjectSpec `json:"spec"`
	// Trigger 触发条件。
	Trigger Trigger `json:"trigger"`
	// Recovery 恢复判定的正常阈值（映射为 JudgeThreshold）。
	Recovery []RecoveryThreshold `json:"recovery"`
	// AutoRevoke 实训结束是否自动撤销注入（默认 true）。
	AutoRevoke *bool `json:"auto_revoke,omitempty"`
}

// ScenarioTarget 注入目标定位。
type ScenarioTarget struct {
	Pod              string `json:"pod,omitempty"`
	Process          string `json:"process,omitempty"`
	NetworkInterface string `json:"network_interface,omitempty"`
}

// RecoveryThreshold 恢复判定阈值（解析后映射为 JudgeThreshold）。
type RecoveryThreshold struct {
	Metric  string  `json:"metric"`
	Service string  `json:"service,omitempty"`
	Op      string  `json:"op"` // lte / gte
	Value   float64 `json:"value"`
}

// ParseScenario 从 JSONB 字节解析剧本并校验。
func ParseScenario(raw []byte) (*Scenario, error) {
	var s Scenario
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("剧本 JSON 解析失败: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// MustParseScenarioJSON 便于测试/种子数据，失败 panic。
func MustParseScenarioJSON(raw string) *Scenario {
	s, err := ParseScenario([]byte(raw))
	if err != nil {
		panic(err)
	}
	return s
}

// Validate 校验剧本必填项与取值合法性。
func (s *Scenario) Validate() error {
	if s.Code == "" {
		return fmt.Errorf("剧本缺少 code")
	}
	if s.Kind == "" {
		return fmt.Errorf("剧本 %s 缺少 kind", s.Code)
	}
	if !supportedKind[s.Kind] {
		return fmt.Errorf("剧本 %s 使用了不支持的故障类型: %s", s.Code, s.Kind)
	}
	if err := s.Trigger.Validate(s.Code); err != nil {
		return err
	}
	if len(s.Recovery) == 0 {
		return fmt.Errorf("剧本 %s 至少需要一个恢复判定阈值", s.Code)
	}
	for i, r := range s.Recovery {
		if r.Op != "lte" && r.Op != "gte" {
			return fmt.Errorf("剧本 %s 恢复阈值[%d] op 必须是 lte/gte", s.Code, i)
		}
		if r.Metric == "" {
			return fmt.Errorf("剧本 %s 恢复阈值[%d] 缺少 metric", s.Code, i)
		}
	}
	return nil
}

// AutoRevokeEnabled 未配置时默认自动撤销。
func (s *Scenario) AutoRevokeEnabled() bool {
	if s.AutoRevoke == nil {
		return true
	}
	return *s.AutoRevoke
}

var supportedKind = map[model.FaultKind]bool{
	model.KindCPU:        true,
	model.KindMem:        true,
	model.KindDisk:       true,
	model.KindNetDelay:   true,
	model.KindNetLoss:    true,
	model.KindProcess:    true,
	model.KindThreadpool: true,
}
