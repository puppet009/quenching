package tests

import (
	"testing"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
)

// ---------- 中等难度剧本（T2-26） ----------

// 剧本#3：慢查询致延迟
const slowQueryScenarioJSON = `{
  "code": "slow-query-order-01",
  "kind": "network_delay",
  "title": "订单慢查询致延迟",
  "target": {"pod": "order-svc", "network_interface": "eth0"},
  "spec": {"kind": "network_delay", "delay_millis": 300},
  "trigger": {"type": "delay", "delay_seconds": 60},
  "recovery": [
    {"metric": "p99_latency", "service": "order-svc", "op": "lte", "value": 200},
    {"metric": "error_rate", "service": "order-svc", "op": "lte", "value": 0.01}
  ],
  "auto_revoke": true
}`

// 剧本#4：网络丢包致级联超时
const netLossScenarioJSON = `{
  "code": "net-loss-product-01",
  "kind": "network_loss",
  "title": "商品服务网络丢包",
  "target": {"pod": "product-svc", "network_interface": "eth0"},
  "spec": {"kind": "network_loss", "loss_percent": 30},
  "trigger": {"type": "metric", "metric": {"metric": "qps", "service": "order-svc", "op": "gt", "value": 20}},
  "recovery": [
    {"metric": "error_rate", "service": "order-svc", "op": "lte", "value": 0.01},
    {"metric": "p99_latency", "service": "order-svc", "op": "lte", "value": 300}
  ],
  "auto_revoke": true
}`

// 剧本#5：内存泄漏
const memLeakScenarioJSON = `{
  "code": "mem-leak-user-01",
  "kind": "memory",
  "title": "用户服务内存泄漏",
  "target": {"pod": "user-svc"},
  "spec": {"kind": "memory", "mem_percent": 85},
  "trigger": {"type": "delay", "delay_seconds": 120},
  "recovery": [
    {"metric": "memory_usage", "service": "user-svc", "op": "lte", "value": 0.7}
  ],
  "auto_revoke": true
}`

func TestMediumScenarioSlowQuery(t *testing.T) {
	s, err := service.ParseScenario([]byte(slowQueryScenarioJSON))
	if err != nil {
		t.Fatalf("剧本#3 慢查询解析失败: %v", err)
	}
	if s.Code != "slow-query-order-01" {
		t.Errorf("code 不匹配: %s", s.Code)
	}
	if s.Kind != model.KindNetDelay {
		t.Errorf("kind 应为 network_delay，got %s", s.Kind)
	}
	if s.Spec.DelayMillis != 300 {
		t.Errorf("延迟应为 300ms，got %d", s.Spec.DelayMillis)
	}
	if s.Trigger.EffectiveType() != service.TriggerDelay {
		t.Errorf("触发类型应为 delay")
	}
	if s.Trigger.DelaySeconds != 60 {
		t.Errorf("延迟 60s，got %d", s.Trigger.DelaySeconds)
	}
	if len(s.Recovery) != 2 {
		t.Errorf("应有 2 个恢复阈值，got %d", len(s.Recovery))
	}
	// 校验恢复阈值
	if s.Recovery[0].Metric != "p99_latency" || s.Recovery[0].Value != 200 {
		t.Error("P99 恢复阈值不对")
	}
	if s.Recovery[1].Metric != "error_rate" || s.Recovery[1].Value != 0.01 {
		t.Error("错误率恢复阈值不对")
	}
	if !s.AutoRevokeEnabled() {
		t.Error("应自动撤销")
	}
}

func TestMediumScenarioNetLoss(t *testing.T) {
	s, err := service.ParseScenario([]byte(netLossScenarioJSON))
	if err != nil {
		t.Fatalf("剧本#4 网络丢包解析失败: %v", err)
	}
	if s.Code != "net-loss-product-01" {
		t.Errorf("code 不匹配: %s", s.Code)
	}
	if s.Kind != model.KindNetLoss {
		t.Errorf("kind 应为 network_loss，got %s", s.Kind)
	}
	if s.Spec.LossPercent != 30 {
		t.Errorf("丢包率应为 30%%，got %d", s.Spec.LossPercent)
	}
	if s.Trigger.EffectiveType() != service.TriggerMetric {
		t.Errorf("触发类型应为 metric，got %s", s.Trigger.EffectiveType())
	}
	if s.Trigger.Metric == nil {
		t.Fatal("metric 触发条件不应为 nil")
	}
	if s.Trigger.Metric.Metric != "qps" || s.Trigger.Metric.Value != 20 {
		t.Errorf("metric 触发条件不对: %+v", s.Trigger.Metric)
	}
	if len(s.Recovery) != 2 {
		t.Errorf("应有 2 个恢复阈值，got %d", len(s.Recovery))
	}
}

func TestMediumScenarioMemLeak(t *testing.T) {
	s, err := service.ParseScenario([]byte(memLeakScenarioJSON))
	if err != nil {
		t.Fatalf("剧本#5 内存泄漏解析失败: %v", err)
	}
	if s.Code != "mem-leak-user-01" {
		t.Errorf("code 不匹配: %s", s.Code)
	}
	if s.Kind != model.KindMem {
		t.Errorf("kind 应为 memory，got %s", s.Kind)
	}
	if s.Spec.MemPercent != 85 {
		t.Errorf("内存占用应为 85%%，got %d", s.Spec.MemPercent)
	}
	if s.Trigger.EffectiveType() != service.TriggerDelay {
		t.Errorf("触发类型应为 delay")
	}
	if s.Trigger.DelaySeconds != 120 {
		t.Errorf("延迟应为 120s，got %d", s.Trigger.DelaySeconds)
	}
	if len(s.Recovery) != 1 {
		t.Errorf("应有 1 个恢复阈值，got %d", len(s.Recovery))
	}
	if s.Recovery[0].Metric != "memory_usage" || s.Recovery[0].Value != 0.7 {
		t.Errorf("内存恢复阈值不对: %+v", s.Recovery[0])
	}
}

// 校验 3 个剧本的目标 pod 都在电商系统中
func TestMediumScenarioTargetValid(t *testing.T) {
	cases := []struct {
		name string
		json string
		pod  string
	}{
		{"慢查询-order", slowQueryScenarioJSON, "order-svc"},
		{"网络丢包-product", netLossScenarioJSON, "product-svc"},
		{"内存泄漏-user", memLeakScenarioJSON, "user-svc"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := service.ParseScenario([]byte(c.json))
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if s.Target.Pod != c.pod {
				t.Errorf("目标 pod 应为 %s，got %s", c.pod, s.Target.Pod)
			}
		})
	}
}

// 校验 3 个剧本的难度特征：触发延迟/条件比简单剧本更复杂
func TestMediumScenarioDifficulty(t *testing.T) {
	// 中等难度特征：
	// - 延迟触发时间更长（给用户更多观察时间）
	// - 恢复阈值多于 1 个（需要多个指标都恢复才算修好）
	// - 或使用 metric 触发（不是简单的延迟触发，有传导过程）

	// 剧本#3：延迟 60s + 2 个恢复阈值（P99 + 错误率）
	s3, _ := service.ParseScenario([]byte(slowQueryScenarioJSON))
	if s3.Trigger.DelaySeconds < 30 {
		t.Error("中等难度延迟触发应 > 30s")
	}
	if len(s3.Recovery) < 2 {
		t.Error("中等难度恢复阈值应 >= 2 个")
	}

	// 剧本#4：metric 触发（需要流量起来才触发，更贴近真实场景）
	s4, _ := service.ParseScenario([]byte(netLossScenarioJSON))
	if s4.Trigger.EffectiveType() != service.TriggerMetric {
		t.Error("网络丢包剧本应使用 metric 触发（级联效应）")
	}

	// 剧本#5：延迟 120s（内存泄漏是渐进的，给用户观察时间）
	s5, _ := service.ParseScenario([]byte(memLeakScenarioJSON))
	if s5.Trigger.DelaySeconds < 60 {
		t.Error("内存泄漏延迟触发应 >= 120s（渐进式）")
	}
}
