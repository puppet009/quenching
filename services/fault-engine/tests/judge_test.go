package tests

import (
	"testing"
	"time"

	"github.com/quenching/fault-engine/internal/service"
)

// 一个 CPU 故障：order-svc 的 cpu_usage 要 <= 70 才算正常，需连续稳定 30s。
func testThresholds() []service.JudgeThreshold {
	return []service.JudgeThreshold{
		{Name: "cpu_usage", Service: "order-svc", Op: service.JudgeOpLTE, Value: 70},
		{Name: "error_rate", Service: "order-svc", Op: service.JudgeOpLTE, Value: 1},
	}
}

func cpu(v float64, e float64) []service.JudgeMetricSample {
	return []service.JudgeMetricSample{
		{Name: "cpu_usage", Service: "order-svc", Value: v},
		{Name: "error_rate", Service: "order-svc", Value: e},
	}
}

// TestStillAbnormal 指标仍超标 -> 未恢复，并给出异常指标。
func TestStillAbnormal(t *testing.T) {
	ev := service.NewJudgeEvaluator(testThresholds(), 30*time.Second)
	now := time.Now()
	r := ev.Evaluate(cpu(95, 5), now)
	if r.Verdict != service.JudgeVerdictNotResolved {
		t.Fatalf("应未恢复，实际 %s", r.Verdict)
	}
	if len(r.Abnormal) != 2 {
		t.Errorf("应报告2个异常指标，实际 %v", r.Abnormal)
	}
}

// TestStableWindowRequiresDuration 达标但没持续够窗口 -> 暂不判定，持续够才判定解决。
func TestStableWindowRequiresDuration(t *testing.T) {
	ev := service.NewJudgeEvaluator(testThresholds(), 30*time.Second)
	base := time.Now()

	r1 := ev.Evaluate(cpu(50, 0.2), base)
	if r1.Verdict != service.JudgeVerdictNotResolved {
		t.Fatal("刚达标不应立即判定，需稳定窗口")
	}
	// 20s 后仍达标，但未满30s
	r2 := ev.Evaluate(cpu(50, 0.2), base.Add(20*time.Second))
	if r2.Verdict != service.JudgeVerdictNotResolved {
		t.Fatal("稳定未满30s不应判定")
	}
	// 31s 持续达标 -> 判定修复
	r3 := ev.Evaluate(cpu(50, 0.2), base.Add(31*time.Second))
	if r3.Verdict != service.JudgeVerdictResolved {
		t.Fatalf("持续达标31s应判定修复，实际 %s", r3.Verdict)
	}
}

// TestSurfaceRecoveryReset 表面恢复：达标一会儿又恶化，连续计时应清零，不判定解决。
func TestSurfaceRecoveryReset(t *testing.T) {
	ev := service.NewJudgeEvaluator(testThresholds(), 30*time.Second)
	base := time.Now()

	ev.Evaluate(cpu(50, 0.2), base)                        // 开始正常
	ev.Evaluate(cpu(50, 0.2), base.Add(20*time.Second))    // 持续达标20s，未满窗口
	r := ev.Evaluate(cpu(90, 8), base.Add(25*time.Second)) // 再次恶化
	if r.Verdict != service.JudgeVerdictNotResolved || len(r.Abnormal) == 0 {
		t.Fatal("二次恶化应判未恢复并列出异常")
	}
	// 恢复正常后需重新累计满 30s（之前的20s应作废）
	ev.Evaluate(cpu(50, 0.2), base.Add(30*time.Second))
	r2 := ev.Evaluate(cpu(50, 0.2), base.Add(40*time.Second)) // 重新累计仅10s
	if r2.Verdict != service.JudgeVerdictNotResolved {
		t.Fatal("恶化后连续计时应重置，10s 不足以判定")
	}
	r3 := ev.Evaluate(cpu(50, 0.2), base.Add(65*time.Second)) // 重新累计满35s
	if r3.Verdict != service.JudgeVerdictResolved {
		t.Fatalf("重新持续达标后应判定，实际 %s", r3.Verdict)
	}
}

// TestPartialRecovery 只修好一个指标不算修复（如CPU降了但错误率仍高）。
func TestPartialRecovery(t *testing.T) {
	ev := service.NewJudgeEvaluator(testThresholds(), 30*time.Second)
	now := time.Now()
	r := ev.Evaluate(cpu(50, 8), now) // CPU 正常，错误率仍 8%>1
	if r.Verdict != service.JudgeVerdictNotResolved {
		t.Fatal("部分恢复不应判定修复")
	}
	found := false
	for _, a := range r.Abnormal {
		if a == "order-svc/error_rate" {
			found = true
		}
	}
	if !found {
		t.Errorf("应指出 error_rate 仍异常，实际 %v", r.Abnormal)
	}
}

// TestMissingMetricTreatedAbnormal 指标采不到（服务可能挂了）不能误判恢复。
func TestMissingMetricTreatedAbnormal(t *testing.T) {
	ev := service.NewJudgeEvaluator(testThresholds(), 30*time.Second)
	r := ev.Evaluate([]service.JudgeMetricSample{
		{Name: "cpu_usage", Service: "order-svc", Value: 50},
		// error_rate 缺失
	}, time.Now())
	if r.Verdict != service.JudgeVerdictNotResolved {
		t.Fatal("指标缺失应视为异常，不能判定恢复")
	}
}

// TestGTEOperator 验证成功率类指标用 gte 方向。
func TestGTEOperator(t *testing.T) {
	th := []service.JudgeThreshold{{Name: "success_rate", Op: service.JudgeOpGTE, Value: 99}}
	ev := service.NewJudgeEvaluator(th, time.Second)
	now := time.Now()
	// 首次达标开始计时（stableFor=0），下一轮采样间隔超过窗口后判定修复。
	r0 := ev.Evaluate([]service.JudgeMetricSample{{Name: "success_rate", Value: 99.5}}, now)
	if r0.Verdict != service.JudgeVerdictNotResolved {
		t.Fatalf("首次达标尚未持续，不应修复，实际 %s", r0.Verdict)
	}
	r := ev.Evaluate([]service.JudgeMetricSample{{Name: "success_rate", Value: 99.6}}, now.Add(2*time.Second))
	if r.Verdict != service.JudgeVerdictResolved {
		t.Fatalf("持续达标2s(>1s窗口)应修复，实际 %s", r.Verdict)
	}
	// 低于阈值则不达标。
	ev2 := service.NewJudgeEvaluator(th, time.Second)
	if r2 := ev2.Evaluate([]service.JudgeMetricSample{{Name: "success_rate", Value: 98}}, now); r2.Verdict != service.JudgeVerdictNotResolved {
		t.Fatalf("成功率98%%不应修复，实际 %s", r2.Verdict)
	}
}
