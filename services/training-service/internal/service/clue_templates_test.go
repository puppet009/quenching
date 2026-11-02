package service

import (
	"strings"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// TestRenderClue_KnownReasons 两类卡顿原因都应给出非空、互不相同的提问式文案。
func TestRenderClue_KnownReasons(t *testing.T) {
	idle := RenderClue(string(model.StallReasonIdle))
	noProgress := RenderClue(string(model.StallReasonNoProgress))

	if idle == "" || noProgress == "" {
		t.Fatal("两类线索文案都不应为空")
	}
	if idle == noProgress {
		t.Fatal("idle 与 no_progress 的线索文案应不同")
	}
}

// TestRenderClue_UnknownReason 未知原因走通用兜底，保证永远能产出一条引导。
func TestRenderClue_UnknownReason(t *testing.T) {
	if got := RenderClue("something_else"); got == "" {
		t.Fatal("未知原因应返回兜底文案，而非空串")
	}
}

// TestRenderClue_NoRootCauseLeak 内容安全红线：任何线索文案都不得出现结论性/答案性措辞。
func TestRenderClue_NoRootCauseLeak(t *testing.T) {
	banned := []string{
		"根因", "root_cause", "root cause",
		"故障是因为", "问题就是", "原因是", "答案",
		// 不得泄露判定阈值/证据数值口径
		"阈值", "错误率为", "p99为", "qps为",
	}
	for _, reason := range []string{string(model.StallReasonIdle), string(model.StallReasonNoProgress), "unknown"} {
		text := RenderClue(reason)
		lower := strings.ToLower(text)
		for _, b := range banned {
			if strings.Contains(lower, strings.ToLower(b)) {
				t.Errorf("reason=%s 的线索文案出现禁用措辞 %q：%s", reason, b, text)
			}
		}
	}
}
