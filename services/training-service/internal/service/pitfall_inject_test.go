package service

import (
	"strings"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// fixedIntn 固定置信度随机源：Intn 恒返回 v（v∈{0,1,2} -> low/medium/high）。
type fixedIntn struct{ v int }

func (r fixedIntn) Intn(int) int { return r.v }

// seqIntn 按顺序返回预设值，用于需要多次调用 Intn 的场景（开场白+结尾判定+结尾选择）。
type seqIntn struct {
	values []int
	idx    int
}

func (r *seqIntn) Intn(n int) int {
	if r.idx >= len(r.values) {
		return 0
	}
	v := r.values[r.idx]
	r.idx++
	// 保证返回值在 [0, n) 范围内
	if v >= n {
		v = n - 1
	}
	return v
}

func injCfg(level string) *model.PitfallConfig {
	return &model.PitfallConfig{
		ID:              42,
		DefectID:        100,
		PitfallType:     model.TypeOverconfidence,
		Name:            "过度自信坑",
		ErrorContent:    "  直接重启就行，不用看日志  ",
		ConfidenceLevel: level,
	}
}

func TestBuildInjectionCarriesFields(t *testing.T) {
	inj := BuildInjection(injCfg(model.ConfidenceHigh), nil)
	if inj.ConfigID != 42 || inj.DefectID != 100 || inj.PitfallType != model.TypeOverconfidence {
		t.Fatalf("注入字段透传错误: %+v", inj)
	}
}

// TestBuildInjectionFixedConfidence 固定档位：开场白属于对应档位的话术库，正文完整。
func TestBuildInjectionFixedConfidence(t *testing.T) {
	levels := []string{model.ConfidenceLow, model.ConfidenceMedium, model.ConfidenceHigh}
	for _, level := range levels {
		// 用 seqIntn 控制：第一次 Intn 选 opening 索引=0，第二次（结尾判定）走100=不命中
		roll := &seqIntn{values: []int{0, 99, 0}}
		inj := BuildInjection(injCfg(level), roll)
		if inj.ConfidenceLevel != level {
			t.Fatalf("固定档位 %s 应原样使用，实际 %s", level, inj.ConfidenceLevel)
		}
		// 开场白必须属于该档位的 openings 列表
		lead, body, ok := strings.Cut(inj.Content, "\n\n")
		if !ok {
			t.Fatalf("档位 %s 正文缺少空行分隔: %q", level, inj.Content)
		}
		if !inSlice(lead, confidencePhrases[level].openings) {
			t.Fatalf("档位 %s 开场白 %q 不在话术库中", level, lead)
		}
		// 当结尾不命中时，body 应等于去空白后的错误建议
		if !strings.HasPrefix(body, "直接重启就行，不用看日志") {
			t.Fatalf("错误建议应附在正文后: %q", body)
		}
		if strings.Contains(inj.Content, "  直接") {
			t.Fatalf("ErrorContent 首尾空白应被裁剪: %q", inj.Content)
		}
	}
}

// TestBuildInjectionRandomConfidence random 档由 roller 选档位，开场白随之变化。
func TestBuildInjectionRandomConfidence(t *testing.T) {
	want := map[int]string{
		0: model.ConfidenceLow,
		1: model.ConfidenceMedium,
		2: model.ConfidenceHigh,
	}
	for rollVal, level := range want {
		// 第一次 Intn(n=3) 选档位，第二次 Intn(n=5) 选 opening=0，第三次结尾判定=99(不命中)
		roll := &seqIntn{values: []int{rollVal, 0, 99, 0}}
		inj := BuildInjection(injCfg(model.ConfidenceRandom), roll)
		if inj.ConfidenceLevel != level {
			t.Fatalf("roller=%d 应解析为 %s，实际 %s", rollVal, level, inj.ConfidenceLevel)
		}
		lead, _, ok := strings.Cut(inj.Content, "\n\n")
		if !ok {
			t.Fatalf("roller=%d 正文格式错误: %q", rollVal, inj.Content)
		}
		if !inSlice(lead, confidencePhrases[level].openings) {
			t.Fatalf("roller=%d 开场白 %q 不属于档位 %s 的话术库", rollVal, lead, level)
		}
	}
}

// TestConfidenceClosingHit 结尾话术命中时，正文末尾有关联的 closing。
func TestConfidenceClosingHit(t *testing.T) {
	// high 档 closingProbability=0.7，roll=50 < 70 应命中
	roll := &seqIntn{values: []int{
		0,  // opening 索引
		50, // 结尾判定：50 < 70 → 命中
		1,  // closing 索引
	}}
	inj := BuildInjection(injCfg(model.ConfidenceHigh), roll)
	// 命中时 content 里应包含对应档位的 closing
	closings := confidencePhrases[model.ConfidenceHigh].closings
	found := false
	for _, c := range closings {
		if strings.Contains(inj.Content, c) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("结尾话术命中时 content 应包含 closing 话术: %q", inj.Content)
	}
}

// TestConfidenceClosingMiss 结尾话术未命中时，正文就是 opening + body。
func TestConfidenceClosingMiss(t *testing.T) {
	// low 档 closingProbability=0.6，roll=80 >= 60 不应命中
	roll := &seqIntn{values: []int{
		0,  // opening 索引
		80, // 结尾判定：80 >= 60 → 不命中
	}}
	inj := BuildInjection(injCfg(model.ConfidenceLow), roll)
	// 未命中时只有 opening + body
	parts := strings.Split(inj.Content, "\n\n")
	if len(parts) != 2 {
		t.Fatalf("结尾未命中时应只有 opening 和 body 两段，实际 %d 段: %q", len(parts), inj.Content)
	}
}

// TestConfidencePhrasesCompleteness 话术库完整性校验。
func TestConfidencePhrasesCompleteness(t *testing.T) {
	levels := []string{model.ConfidenceLow, model.ConfidenceMedium, model.ConfidenceHigh}
	for _, level := range levels {
		phrases, ok := confidencePhrases[level]
		if !ok {
			t.Fatalf("档位 %s 缺少话术配置", level)
		}
		if len(phrases.openings) < 3 {
			t.Fatalf("档位 %s 开场白数量不足，至少3句，实际 %d", level, len(phrases.openings))
		}
		if len(phrases.closings) < 2 {
			t.Fatalf("档位 %s 结尾话术数量不足，至少2句，实际 %d", level, len(phrases.closings))
		}
		if phrases.closingProbability <= 0 || phrases.closingProbability > 1 {
			t.Fatalf("档位 %s 结尾概率非法: %f", level, phrases.closingProbability)
		}
	}
}

func TestBuildInjectionEmptyLevelDefaultsRandom(t *testing.T) {
	// 缺省档位按 random 处理（resolveConfidence default 分支）。
	inj := BuildInjection(injCfg(""), &seqIntn{values: []int{0, 0, 99}})
	if inj.ConfidenceLevel != model.ConfidenceLow {
		t.Fatalf("缺省档位应走随机分支，实际 %s", inj.ConfidenceLevel)
	}
}

func TestInjectionString(t *testing.T) {
	inj := BuildInjection(injCfg(model.ConfidenceHigh), nil)
	s := inj.String()
	if !strings.Contains(s, "config=42") || !strings.Contains(s, "defect=100") ||
		!strings.Contains(s, model.TypeOverconfidence) || !strings.Contains(s, "confidence=high") {
		t.Fatalf("日志串缺少关键信息: %s", s)
	}
}

// inSlice 判断 s 是否在 list 中。
func inSlice(s string, list []string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
