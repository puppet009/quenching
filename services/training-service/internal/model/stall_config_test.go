// 卡顿检测配置归一化白盒单测（T3-6）：默认常量为包内未导出名，
// 归一化逻辑与默认值的对应关系放在 model 包内验证。
package model

import (
	"testing"
	"time"
)

func TestNormalizeConfig(t *testing.T) {
	got := StallConfig{}.Normalize()
	if got.IdleTimeout != defaultIdleTimeout || got.ProgressWindow != defaultProgressWindow {
		t.Fatalf("默认时长未填充: %+v", got)
	}
	if got.MinActivities != defaultMinActivities || got.MinSegmentPoints != defaultMinSegmentPoints {
		t.Fatalf("默认次数未填充: %+v", got)
	}
	if got.MinErrorRateDrop != defaultErrorRateDropPct || got.MinP99Drop != defaultP99DropSec {
		t.Fatalf("默认阈值未填充: %+v", got)
	}
	if got.MinQPSRelativeGain != defaultQPSRelativeGain || got.MinQPSAbsoluteGain != defaultQPSAbsoluteGain {
		t.Fatalf("默认 QPS 阈值未填充: %+v", got)
	}

	// 负值也要被默认值兜住。
	got = (StallConfig{IdleTimeout: -1, MinActivities: -3}).Normalize()
	if got.IdleTimeout != defaultIdleTimeout || got.MinActivities != defaultMinActivities {
		t.Fatalf("负值未被默认值兜住: %+v", got)
	}

	// 显式配置应原样保留。
	custom := StallConfig{IdleTimeout: 7 * time.Minute, AbandonTimeout: 30 * time.Minute,
		ProgressWindow: 12 * time.Minute, MinActivities: 3, MinSegmentPoints: 3}
	if got = custom.Normalize(); got != custom {
		t.Fatalf("显式配置被改写: 期望 %+v 实得 %+v", custom, got)
	}

	// 弃班线不得早于卡顿线，否则弱引导还没来得及推就被判结束。
	got = StallConfig{IdleTimeout: 7 * time.Minute, AbandonTimeout: 2 * time.Minute}.Normalize()
	if got.AbandonTimeout != 7*time.Minute {
		t.Fatalf("AbandonTimeout 未被抬到 IdleTimeout: %+v", got)
	}
}
