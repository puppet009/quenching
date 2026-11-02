// 难度参数系统模型。
//
// 三档难度：简单 / 普通 / 困难
// 影响维度：
//   - 故障注入烈度（倍率）
//   - 告警灵敏度（阈值高低）
//   - 日志完整度（级别和详细程度）
//   - AI 提示次数
//   - 干扰项（额外噪音/干扰故障）
package model

import "fmt"

// DifficultyLevel 难度等级。
type DifficultyLevel string

const (
	LevelEasy   DifficultyLevel = "easy"   // 简单
	LevelNormal DifficultyLevel = "normal" // 普通（默认）
	LevelHard   DifficultyLevel = "hard"   // 困难
)

// IsValid 校验难度等级是否合法。
func IsValid(level string) bool {
	switch DifficultyLevel(level) {
	case LevelEasy, LevelNormal, LevelHard:
		return true
	default:
		return false
	}
}

// DifficultyConfig 一个难度等级的完整参数配置。
type DifficultyConfig struct {
	Level       DifficultyLevel `json:"level"`
	Name        string          `json:"name"`        // 中文名
	Description string          `json:"description"` // 一句话描述

	// ——— 故障注入烈度 ———
	// FaultIntensity 故障强度倍率（0.5-2.0）。
	// 简单：烈度低，容易观察到恢复
	// 困难：烈度高，恢复更难
	FaultIntensity float64 `json:"fault_intensity"`

	// ——— 告警灵敏度 ———
	// AlertSensitivity 告警触发灵敏度阈值（0.0-1.0）。
	// 值越低越灵敏（简单模式告警多，容易发现问题）
	// 值越高越迟钝（困难模式告警少，需要自己找）
	AlertSensitivity float64 `json:"alert_sensitivity"`
	// AlertCountMultiplier 告警数量倍率。
	// 简单模式告警多且详细，困难模式告警稀少
	AlertCountMultiplier float64 `json:"alert_count_multiplier"`

	// ——— 日志完整度 ———
	// LogLevel 日志级别：debug / info / warn / error
	// 简单模式：debug 级别，日志最全
	// 困难模式：error 级别，需要自己推断
	LogLevel string `json:"log_level"`
	// LogVerbosity 日志详细程度 0-3
	// 3=最全（含堆栈、请求体），0=最简
	LogVerbosity int `json:"log_verbosity"`

	// ——— AI 提示 ———
	// HintCount AI 提示总次数限制
	HintCount int `json:"hint_count"`
	// ProgressiveHintLevels 渐进式提示解锁的级数（1/2/3）
	ProgressiveHintLevels int `json:"progressive_hint_levels"`

	// ——— 干扰项 ———
	// HasDistractions 是否有额外干扰（无关告警/日志噪音）
	HasDistractions bool `json:"has_distractions"`
	// DistractionLevel 干扰强度 0-1
	DistractionLevel float64 `json:"distraction_level"`

	// ——— 时间 ———
	// TimeLimitMinutes 训练时间限制（分钟），0 表示不限制
	TimeLimitMinutes int `json:"time_limit_minutes"`
}

// presets 三档难度预设配置。
var presets = map[DifficultyLevel]DifficultyConfig{
	LevelEasy: {
		Level:       LevelEasy,
		Name:        "简单",
		Description: "告警清晰、日志完整、烈度温和，适合入门学习",

		FaultIntensity:        0.6, // 烈度低，现象明显但不爆炸
		AlertSensitivity:      0.3, // 低阈值就触发告警
		AlertCountMultiplier:  1.5, // 告警数量多 50%
		LogLevel:              "debug",
		LogVerbosity:          3, // 最详细日志
		HintCount:             8, // 提示次数充足
		ProgressiveHintLevels: 3, // 三级提示全开
		HasDistractions:       false,
		DistractionLevel:      0,
		TimeLimitMinutes:      0, // 不设时间限制
	},
	LevelNormal: {
		Level:       LevelNormal,
		Name:        "普通",
		Description: "标准配置，适合有一定基础的工程师",

		FaultIntensity:        1.0, // 标准烈度
		AlertSensitivity:      0.5, // 标准告警阈值
		AlertCountMultiplier:  1.0, // 标准告警数量
		LogLevel:              "info",
		LogVerbosity:          2, // 标准详细度
		HintCount:             5, // 适中提示次数
		ProgressiveHintLevels: 2, // 两级提示
		HasDistractions:       false,
		DistractionLevel:      0,
		TimeLimitMinutes:      0, // 不设时间限制
	},
	LevelHard: {
		Level:       LevelHard,
		Name:        "困难",
		Description: "告警稀少、日志残缺、烈度强、有干扰，挑战真实排障能力",

		FaultIntensity:        1.5, // 烈度更强，恢复更难
		AlertSensitivity:      0.8, // 高阈值才告警，很多情况不告警
		AlertCountMultiplier:  0.4, // 告警数量只有 40%
		LogLevel:              "warn",
		LogVerbosity:          1, // 日志较简略
		HintCount:             3, // 提示次数少
		ProgressiveHintLevels: 1, // 只有一级提示（方向引导）
		HasDistractions:       true,
		DistractionLevel:      0.6, // 中等干扰强度
		TimeLimitMinutes:      45,  // 45 分钟时间限制
	},
}

// GetConfig 获取指定难度的配置。
// 如果难度不合法，返回普通难度配置和错误。
func GetConfig(level string) (DifficultyConfig, error) {
	if !IsValid(level) {
		return presets[LevelNormal], fmt.Errorf("不支持的难度等级: %s", level)
	}
	return presets[DifficultyLevel(level)], nil
}

// MustGet 获取指定难度配置，非法时返回普通难度（不报错）。
func MustGet(level string) DifficultyConfig {
	cfg, _ := GetConfig(level)
	return cfg
}

// ListAll 返回所有难度配置（前端展示选项用）。
func ListAll() []DifficultyConfig {
	return []DifficultyConfig{
		presets[LevelEasy],
		presets[LevelNormal],
		presets[LevelHard],
	}
}

// DefaultLevel 默认难度（普通）。
func DefaultLevel() DifficultyLevel {
	return LevelNormal
}
