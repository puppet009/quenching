package model

import "testing"

// ---- ResolveConsequence：开关与默认规格 ----

func TestResolveConsequenceDisabled(t *testing.T) {
	fr := FallRules{AdoptActions: []string{"restart_service"}}
	if got := fr.ResolveConsequence(SeverityHigh); got != nil {
		t.Fatalf("worsen_fault=false 不应产生次生故障, got %+v", got)
	}
}

func TestResolveConsequenceDefaultCPU(t *testing.T) {
	// worsen_fault=true 但未配 consequence：错误命令致额外负载 -> 默认 CPU 满载，
	// 强度按 severity 归一（medium=80）。
	fr := FallRules{WorsenFault: true}
	got := fr.ResolveConsequence(SeverityMedium)
	if got == nil {
		t.Fatal("应返回默认次生故障规格")
	}
	if got.Kind != ConsequenceCPU || got.CPUPercent != 80 {
		t.Fatalf("默认应为 medium 强度 CPU 80%%, got kind=%s cpu=%d", got.Kind, got.CPUPercent)
	}
	if got.DurationSec != 0 {
		t.Fatalf("默认 duration_sec=0（平台掌控，会话结束撤销）, got %d", got.DurationSec)
	}
}

func TestResolveConsequenceSeverityLadder(t *testing.T) {
	cases := []struct {
		sev  string
		want int
	}{
		{SeverityLow, 60},
		{SeverityMedium, 80},
		{SeverityHigh, 100},
		{"", 80}, // 缺省/非法 severity 归一为 medium
		{"xxx", 80},
	}
	for _, tc := range cases {
		got := (FallRules{WorsenFault: true}).ResolveConsequence(tc.sev)
		if got.CPUPercent != tc.want {
			t.Errorf("severity=%q 期望 CPU %d, got %d", tc.sev, tc.want, got.CPUPercent)
		}
	}
}

// ---- 各故障类型按 severity 补强度 ----

func TestResolveConsequenceKindsBySeverity(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		sev      string
		check    func(*ConsequenceFault) bool
		wantDesc string
	}{
		{"memory-medium", ConsequenceMemory, SeverityMedium, func(q *ConsequenceFault) bool { return q.MemPercent == 75 }, "mem=75"},
		{"disk-low", ConsequenceDisk, SeverityLow, func(q *ConsequenceFault) bool { return q.DiskPercent == 50 }, "disk=50"},
		{"disk-high", ConsequenceDisk, SeverityHigh, func(q *ConsequenceFault) bool { return q.DiskPercent == 90 }, "disk=90"},
		{"delay-low", ConsequenceNetDelay, SeverityLow, func(q *ConsequenceFault) bool { return q.DelayMillis == 200 }, "delay=200ms"},
		{"delay-high", ConsequenceNetDelay, SeverityHigh, func(q *ConsequenceFault) bool { return q.DelayMillis == 1000 }, "delay=1000ms"},
		{"loss-medium", ConsequenceNetLoss, SeverityMedium, func(q *ConsequenceFault) bool { return q.LossPercent == 30 }, "loss=30"},
		{"cpu-low", ConsequenceCPU, SeverityLow, func(q *ConsequenceFault) bool { return q.CPUPercent == 60 }, "cpu=60"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := FallRules{WorsenFault: true, Consequence: &ConsequenceFault{Kind: tc.kind}}
			got := fr.ResolveConsequence(tc.sev)
			if got.Kind != tc.kind || !tc.check(got) {
				t.Fatalf("%s 期望 %s, got kind=%s cpu=%d mem=%d disk=%d delay=%d loss=%d",
					tc.name, tc.wantDesc, got.Kind, got.CPUPercent, got.MemPercent,
					got.DiskPercent, got.DelayMillis, got.LossPercent)
			}
		})
	}
}

func TestResolveConsequenceKeepsExplicitIntensity(t *testing.T) {
	// 显式配置的非零强度不能被 severity 缺省覆盖。
	fr := FallRules{WorsenFault: true, Consequence: &ConsequenceFault{
		Kind: ConsequenceCPU, CPUPercent: 42, DurationSec: 30,
	}}
	got := fr.ResolveConsequence(SeverityHigh)
	if got.CPUPercent != 42 {
		t.Fatalf("显式强度 42 不应被覆盖为 %d", got.CPUPercent)
	}
	if got.DurationSec != 30 {
		t.Fatalf("显式 duration_sec=30 应保留, got %d", got.DurationSec)
	}
}

// ---- Validate ----

func TestConsequenceValidate(t *testing.T) {
	var nilQ *ConsequenceFault
	if err := nilQ.Validate(1); err != nil {
		t.Fatalf("nil 规格应放行: %v", err)
	}
	good := []ConsequenceFault{
		{Kind: ConsequenceCPU, CPUPercent: 100},
		{Kind: ConsequenceMemory, MemPercent: 0},
		{Kind: ConsequenceDisk, DiskPercent: 100},
		{Kind: ConsequenceNetDelay, DelayMillis: 500, DurationSec: 0},
		{Kind: ConsequenceNetLoss, LossPercent: 50},
	}
	for i, q := range good {
		if err := q.Validate(7); err != nil {
			t.Errorf("case %d 合法规格被拒: %v", i, err)
		}
	}
	bad := []ConsequenceFault{
		{Kind: "process_kill"},                       // 不开放的类型
		{Kind: "bogus"},                              // 非法类型
		{Kind: ConsequenceCPU, CPUPercent: 101},      // 超界
		{Kind: ConsequenceMemory, MemPercent: -1},    // 负值
		{Kind: ConsequenceDisk, DiskPercent: 120},    // 超界
		{Kind: ConsequenceNetLoss, LossPercent: -5},  // 负值
		{Kind: ConsequenceNetDelay, DelayMillis: -1}, // 负时长
		{Kind: ConsequenceCPU, DurationSec: -1},      // 负时长
	}
	for i, q := range bad {
		if err := q.Validate(7); err == nil {
			t.Errorf("case %d 非法规格应被拒: %+v", i, q)
		}
	}
}

// ---- 端到端：JSONB 内联 consequence 能被 Parse 解析并级联校验 ----

func TestParseConsequenceInline(t *testing.T) {
	raw := `{
		"id": 9, "defect_id": 100, "pitfall_type": "causal_misjudgment",
		"name": "错误重启", "error_content": "重启试试", "status": "active",
		"trigger_condition": {"when": "always", "probability": 1},
		"recognition_rules": {"dispute_keywords": ["不对"]},
		"pitfall_rules": {
			"adopt_actions": ["restart_service"],
			"worsen_fault": true,
			"consequence": {"kind": "network_loss", "loss_percent": 35}
		},
		"severity": "high"
	}`
	c, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("内联 consequence 应可解析: %v", err)
	}
	if c.PitfallRules.Consequence == nil || c.PitfallRules.Consequence.Kind != ConsequenceNetLoss {
		t.Fatalf("consequence 未正确解析: %+v", c.PitfallRules.Consequence)
	}
	got := c.PitfallRules.ResolveConsequence(c.Severity)
	if got.LossPercent != 35 {
		t.Fatalf("显式 loss=35 应保留, got %d", got.LossPercent)
	}
}

func TestParseConsequenceBadKindRejected(t *testing.T) {
	raw := `{
		"id": 9, "defect_id": 100, "pitfall_type": "info_omission",
		"name": "错误配置", "error_content": "改这个", "status": "active",
		"trigger_condition": {"when": "always", "probability": 1},
		"recognition_rules": {"dispute_keywords": ["不对"]},
		"pitfall_rules": {
			"adopt_keywords": ["好的"],
			"worsen_fault": true,
			"consequence": {"kind": "process_kill"}
		}
	}`
	if _, err := Parse([]byte(raw)); err == nil {
		t.Fatal("非法 consequence kind 应在加载期被拒")
	}
}
