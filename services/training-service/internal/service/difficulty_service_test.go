package service

import (
	"context"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// TestGetConfig_Easy 验证简单难度参数。
func TestGetConfig_Easy(t *testing.T) {
	cfg, err := model.GetConfig("easy")
	if err != nil {
		t.Fatalf("获取简单难度失败: %v", err)
	}
	if cfg.Level != model.LevelEasy {
		t.Errorf("level 应为 easy, got %s", cfg.Level)
	}
	if cfg.Name != "简单" {
		t.Errorf("name 应为 简单, got %s", cfg.Name)
	}
	if cfg.FaultIntensity != 0.6 {
		t.Errorf("简单烈度应为 0.6, got %f", cfg.FaultIntensity)
	}
	if cfg.HintCount != 8 {
		t.Errorf("简单提示次数应为 8, got %d", cfg.HintCount)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("简单日志级别应为 debug, got %s", cfg.LogLevel)
	}
	if cfg.LogVerbosity != 3 {
		t.Errorf("简单日志详细度应为 3, got %d", cfg.LogVerbosity)
	}
	if cfg.HasDistractions {
		t.Error("简单模式不应有干扰项")
	}
	if cfg.AlertSensitivity >= 0.5 {
		t.Errorf("简单模式告警应更灵敏（低阈值）, got %f", cfg.AlertSensitivity)
	}
}

// TestGetConfig_Normal 验证普通难度参数（默认）。
func TestGetConfig_Normal(t *testing.T) {
	cfg, err := model.GetConfig("normal")
	if err != nil {
		t.Fatalf("获取普通难度失败: %v", err)
	}
	if cfg.FaultIntensity != 1.0 {
		t.Errorf("普通烈度应为 1.0, got %f", cfg.FaultIntensity)
	}
	if cfg.HintCount != 5 {
		t.Errorf("普通提示次数应为 5, got %d", cfg.HintCount)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("普通日志级别应为 info, got %s", cfg.LogLevel)
	}
}

// TestGetConfig_Hard 验证困难难度参数。
func TestGetConfig_Hard(t *testing.T) {
	cfg, err := model.GetConfig("hard")
	if err != nil {
		t.Fatalf("获取困难难度失败: %v", err)
	}
	if cfg.Level != model.LevelHard {
		t.Errorf("level 应为 hard, got %s", cfg.Level)
	}
	if cfg.FaultIntensity != 1.5 {
		t.Errorf("困难烈度应为 1.5, got %f", cfg.FaultIntensity)
	}
	if cfg.HintCount != 3 {
		t.Errorf("困难提示次数应为 3, got %d", cfg.HintCount)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("困难日志级别应为 warn, got %s", cfg.LogLevel)
	}
	if !cfg.HasDistractions {
		t.Error("困难模式应有干扰项")
	}
	if cfg.TimeLimitMinutes != 45 {
		t.Errorf("困难模式时间限制应为 45 分钟, got %d", cfg.TimeLimitMinutes)
	}
	if cfg.AlertSensitivity <= 0.5 {
		t.Errorf("困难模式告警应更迟钝（高阈值）, got %f", cfg.AlertSensitivity)
	}
}

// TestGetConfig_Invalid 非法难度返回错误。
func TestGetConfig_Invalid(t *testing.T) {
	cfg, err := model.GetConfig("extreme")
	if err == nil {
		t.Error("非法难度应返回错误")
	}
	// 错误时返回默认（普通）配置
	if cfg.Level != model.LevelNormal {
		t.Errorf("非法难度 fallback 应为 normal, got %s", cfg.Level)
	}
}

// TestIsValid 难度校验。
func TestIsValid(t *testing.T) {
	tests := []struct {
		level string
		want  bool
	}{
		{"easy", true},
		{"normal", true},
		{"hard", true},
		{"Easy", false},
		{"", false},
		{"nightmare", false},
	}
	for _, tt := range tests {
		if got := model.IsValid(tt.level); got != tt.want {
			t.Errorf("IsValid(%q) = %v, want %v", tt.level, got, tt.want)
		}
	}
}

// TestDifficultyGradient 三档难度单调递增/递减关系验证。
func TestDifficultyGradient(t *testing.T) {
	easy, _ := model.GetConfig("easy")
	normal, _ := model.GetConfig("normal")
	hard, _ := model.GetConfig("hard")

	// 烈度：简单 < 普通 < 困难
	if !(easy.FaultIntensity < normal.FaultIntensity && normal.FaultIntensity < hard.FaultIntensity) {
		t.Error("烈度应随难度递增")
	}

	// 提示次数：简单 > 普通 > 困难
	if !(easy.HintCount > normal.HintCount && normal.HintCount > hard.HintCount) {
		t.Error("提示次数应随难度递减")
	}

	// 告警灵敏度（值越低越灵敏）：简单 < 普通 < 困难
	if !(easy.AlertSensitivity < normal.AlertSensitivity && normal.AlertSensitivity < hard.AlertSensitivity) {
		t.Error("告警阈值应随难度递增（灵敏度递减）")
	}

	// 日志详细度：简单 > 普通 > 困难
	if !(easy.LogVerbosity >= normal.LogVerbosity && normal.LogVerbosity >= hard.LogVerbosity) {
		t.Error("日志详细度应随难度递减")
	}
}

// TestApplyToFaultSpec_Delay 应用难度到网络延迟参数。
func TestApplyToFaultSpec_Delay(t *testing.T) {
	svc := NewDifficultyService()
	delay := 200 // 基础 200ms 延迟

	spec := &FaultSpec{
		Code:        "test-delay",
		Kind:        "network_delay",
		DelayMillis: &delay,
	}

	// 简单模式：200 × 0.6 = 120ms
	easyResult, err := svc.ApplyToFaultSpec(context.Background(), spec, "easy")
	if err != nil {
		t.Fatalf("应用简单难度失败: %v", err)
	}
	if *easyResult.Spec.DelayMillis != 120 {
		t.Errorf("简单模式延迟应为 120ms, got %d", *easyResult.Spec.DelayMillis)
	}

	// 普通模式：200 × 1.0 = 200ms
	normalResult, _ := svc.ApplyToFaultSpec(context.Background(), spec, "normal")
	if *normalResult.Spec.DelayMillis != 200 {
		t.Errorf("普通模式延迟应为 200ms, got %d", *normalResult.Spec.DelayMillis)
	}

	// 困难模式：200 × 1.5 = 300ms
	hardResult, _ := svc.ApplyToFaultSpec(context.Background(), spec, "hard")
	if *hardResult.Spec.DelayMillis != 300 {
		t.Errorf("困难模式延迟应为 300ms, got %d", *hardResult.Spec.DelayMillis)
	}

	// 原参数不应被修改
	if *spec.DelayMillis != 200 {
		t.Error("原始 FaultSpec 不应被修改")
	}
}

// TestApplyToFaultSpec_Recovery 恢复阈值反向调整验证。
// 难度越高，恢复阈值越严格（越小），越难达标。
func TestApplyToFaultSpec_Recovery(t *testing.T) {
	svc := NewDifficultyService()
	latency := 200.0 // 基础恢复阈值 200ms

	spec := &FaultSpec{
		Code:              "test-recovery",
		RecoveryLatencyMs: &latency,
	}

	easyResult, _ := svc.ApplyToFaultSpec(context.Background(), spec, "easy")
	hardResult, _ := svc.ApplyToFaultSpec(context.Background(), spec, "hard")

	// 简单模式阈值更宽松（更大），困难模式更严格（更小）
	if *easyResult.Spec.RecoveryLatencyMs <= *hardResult.Spec.RecoveryLatencyMs {
		t.Errorf("简单模式恢复阈值应大于困难模式: easy=%f, hard=%f",
			*easyResult.Spec.RecoveryLatencyMs, *hardResult.Spec.RecoveryLatencyMs)
	}
}

// TestApplyToFaultSpec_NilSpec 空参数返回错误。
func TestApplyToFaultSpec_NilSpec(t *testing.T) {
	svc := NewDifficultyService()
	_, err := svc.ApplyToFaultSpec(context.Background(), nil, "normal")
	if err == nil {
		t.Error("空 FaultSpec 应返回错误")
	}
}

// TestApplyToFaultSpec_InvalidLevel 非法难度返回错误。
func TestApplyToFaultSpec_InvalidLevel(t *testing.T) {
	svc := NewDifficultyService()
	delay := 100
	spec := &FaultSpec{DelayMillis: &delay}
	_, err := svc.ApplyToFaultSpec(context.Background(), spec, "impossible")
	if err == nil {
		t.Error("非法难度应返回错误")
	}
}

// TestApplyIntensity_Bounds 烈度边界限制。
func TestApplyIntensity_Bounds(t *testing.T) {
	// 极小值应被限制在 min
	result := applyIntensity(10, 0.1, 50, 5000)
	if result < 50 {
		t.Errorf("烈度应被限制在最小值以上, got %f", result)
	}

	// 极大值应被限制在 max
	result2 := applyIntensity(5000, 2.0, 50, 5000)
	if result2 > 5000 {
		t.Errorf("烈度应被限制在最大值以下, got %f", result2)
	}
}

// TestListAll 返回所有三档配置。
func TestListAll(t *testing.T) {
	list := model.ListAll()
	if len(list) != 3 {
		t.Errorf("应有 3 个难度等级, got %d", len(list))
	}

	levels := map[model.DifficultyLevel]bool{}
	for _, cfg := range list {
		levels[cfg.Level] = true
	}
	if !levels[model.LevelEasy] || !levels[model.LevelNormal] || !levels[model.LevelHard] {
		t.Error("三档难度应都存在")
	}
}

// TestMustGet 非法难度返回默认配置。
func TestMustGet(t *testing.T) {
	cfg := model.MustGet("unknown")
	if cfg.Level != model.LevelNormal {
		t.Errorf("MustGet 非法难度应返回 normal, got %s", cfg.Level)
	}

	cfg2 := model.MustGet("hard")
	if cfg2.Level != model.LevelHard {
		t.Errorf("MustGet 合法难度应正常返回, got %s", cfg2.Level)
	}
}

// TestFaultSpecJSON 序列化与反序列化。
func TestFaultSpecJSON(t *testing.T) {
	delay := 300
	loss := 20.0
	spec := &FaultSpec{
		Code:        "test-spec",
		Kind:        "network_delay",
		DelayMillis: &delay,
		LossPercent: &loss,
	}

	s, err := MarshalFaultSpec(spec)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if s == "" {
		t.Fatal("序列化结果不应为空")
	}

	parsed, err := UnmarshalFaultSpec(s)
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if parsed.Code != "test-spec" {
		t.Errorf("code 不匹配: %s", parsed.Code)
	}
	if *parsed.DelayMillis != 300 {
		t.Errorf("delay 不匹配: %d", *parsed.DelayMillis)
	}
	if *parsed.LossPercent != 20.0 {
		t.Errorf("loss 不匹配: %f", *parsed.LossPercent)
	}
}
