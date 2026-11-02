package tests

import (
	"testing"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
)

const validScenarioJSON = `{
  "code": "cpu-fullload-01",
  "kind": "cpu",
  "title": "CPU满载",
  "target": {"pod": "order-svc"},
  "spec": {"kind": "cpu", "cpu_percent": 90},
  "trigger": {"type": "delay", "delay_seconds": 30},
  "recovery": [{"metric": "cpu_usage", "service": "order-svc", "op": "lte", "value": 70}],
  "auto_revoke": true
}`

func TestParseScenarioOK(t *testing.T) {
	s, err := service.ParseScenario([]byte(validScenarioJSON))
	if err != nil {
		t.Fatalf("合法剧本解析失败: %v", err)
	}
	if s.Code != "cpu-fullload-01" || s.Kind != model.KindCPU {
		t.Fatal("基础字段解析错误")
	}
	if s.Trigger.EffectiveType() != service.TriggerDelay || s.Trigger.DelaySeconds != 30 {
		t.Fatal("触发条件解析错误")
	}
	if !s.AutoRevokeEnabled() {
		t.Fatal("auto_revoke 应为 true")
	}
}

func TestParseScenarioInvalid(t *testing.T) {
	bad := []struct {
		name string
		json string
	}{
		{"缺code", `{"kind":"cpu","trigger":{"type":"delay"},"recovery":[{"metric":"a","op":"lte","value":1}]}`},
		{"未知故障类型", `{"code":"x","kind":"alien","trigger":{"type":"delay"},"recovery":[{"metric":"a","op":"lte","value":1}]}`},
		{"缺恢复阈值", `{"code":"x","kind":"cpu","trigger":{"type":"delay"}}`},
		{"非法op", `{"code":"x","kind":"cpu","trigger":{"type":"delay"},"recovery":[{"metric":"a","op":"==","value":1}]}`},
		{"metric触发缺条件", `{"code":"x","kind":"cpu","trigger":{"type":"metric"},"recovery":[{"metric":"a","op":"lte","value":1}]}`},
		{"窗口非法", `{"code":"x","kind":"cpu","trigger":{"type":"window","window":{"start_seconds":20,"end_seconds":10}},"recovery":[{"metric":"a","op":"lte","value":1}]}`},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if _, err := service.ParseScenario([]byte(c.json)); err == nil {
				t.Fatalf("非法剧本应报错: %s", c.name)
			}
		})
	}
}

func TestDelayTrigger(t *testing.T) {
	s := service.MustParseScenarioJSON(validScenarioJSON)
	st := service.NewTriggerState(s)

	if d := st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 29}); d != service.TriggerDecisionWait {
		t.Errorf("29s 不应触发，实际 %v", d)
	}
	if d := st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 30}); d != service.TriggerDecisionFire {
		t.Errorf("30s 应触发，实际 %v", d)
	}
}

func TestTriggerOnlyOnce(t *testing.T) {
	s := service.MustParseScenarioJSON(validScenarioJSON)
	st := service.NewTriggerState(s)
	if st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 30}) != service.TriggerDecisionFire {
		t.Fatal("应触发")
	}
	st.MarkFired()
	// 之后无论多久都不再触发
	for _, sec := range []int{31, 60, 1000} {
		if d := st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: sec}); d != service.TriggerDecisionWait {
			t.Errorf("已触发后不应再触发，%ds 实际 %v", sec, d)
		}
	}
}

func TestMetricTrigger(t *testing.T) {
	json := `{
  "code":"qps-spike","kind":"cpu",
  "trigger":{"type":"metric","metric":{"metric":"qps","service":"order-svc","op":"gt","value":500}},
  "recovery":[{"metric":"cpu_usage","op":"lte","value":70}]}`
	s := service.MustParseScenarioJSON(json)
	st := service.NewTriggerState(s)

	low := []service.JudgeMetricSample{{Name: "qps", Service: "order-svc", Value: 300}}
	high := []service.JudgeMetricSample{{Name: "qps", Service: "order-svc", Value: 600}}

	if st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 10, Metrics: low}) != service.TriggerDecisionWait {
		t.Error("QPS未达标不应触发")
	}
	if st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 10, Metrics: high}) != service.TriggerDecisionFire {
		t.Error("QPS超过500应触发")
	}
}

func TestWindowTriggerMissed(t *testing.T) {
	// 窗口 [20,40)，纯时间窗（无指标条件）
	json := `{
  "code":"win","kind":"cpu",
  "trigger":{"type":"window","window":{"start_seconds":20,"end_seconds":40}},
  "recovery":[{"metric":"cpu_usage","op":"lte","value":70}]}`
	s := service.MustParseScenarioJSON(json)
	st := service.NewTriggerState(s)

	if st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 19}) != service.TriggerDecisionWait {
		t.Error("窗口未到应等待")
	}
	if st.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 25}) != service.TriggerDecisionFire {
		t.Error("进入窗口应触发")
	}
	st.MarkFired()

	// 另一个实例：窗口内一直没评估，过了结束时间 -> 过期放弃
	st2 := service.NewTriggerState(s)
	if d := st2.Evaluate(service.TriggerRuntimeContext{ElapsedSeconds: 41}); d != service.TriggerDecisionExpired {
		t.Errorf("错过窗口应 Expired，实际 %v", d)
	}
}

func TestWindowWithMetricCondition(t *testing.T) {
	// 窗口 [20,60) 且需要 QPS>500
	json := `{
  "code":"win-metric","kind":"cpu",
  "trigger":{"type":"window","window":{"start_seconds":20,"end_seconds":60},
             "metric":{"metric":"qps","op":"gt","value":500}},
  "recovery":[{"metric":"cpu_usage","op":"lte","value":70}]}`
	s := service.MustParseScenarioJSON(json)
	st := service.NewTriggerState(s)

	inWindowLowQPS := service.TriggerRuntimeContext{ElapsedSeconds: 30, Metrics: []service.JudgeMetricSample{{Name: "qps", Value: 100}}}
	if st.Evaluate(inWindowLowQPS) != service.TriggerDecisionWait {
		t.Error("窗口内但指标不满足应等待")
	}
	inWindowHighQPS := service.TriggerRuntimeContext{ElapsedSeconds: 35, Metrics: []service.JudgeMetricSample{{Name: "qps", Value: 600}}}
	if st.Evaluate(inWindowHighQPS) != service.TriggerDecisionFire {
		t.Error("窗口内指标满足应触发")
	}
}
