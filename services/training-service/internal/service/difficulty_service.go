// 难度参数服务。
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/quenching/training-service/internal/model"
)

// FaultSpec 故障剧本原始参数（从 fault-engine 拿到的基础配置）。
// 只保留难度会影响的字段，其他字段透传。
type FaultSpec struct {
	Code  string `json:"code"`
	Kind  string `json:"kind"`
	Title string `json:"title"`

	// 注入相关参数
	DelayMillis     *int     `json:"delay_millis,omitempty"`      // 网络延迟 ms
	LossPercent     *float64 `json:"loss_percent,omitempty"`      // 丢包率 %
	MemoryPercent   *float64 `json:"memory_percent,omitempty"`    // 内存占用 %
	CPUPercent      *int     `json:"cpu_percent,omitempty"`       // CPU 占用 %
	DiskFillPercent *float64 `json:"disk_fill_percent,omitempty"` // 磁盘占用 %

	// 触发配置
	TriggerDelaySeconds *int `json:"trigger_delay_seconds,omitempty"` // 延迟触发秒数

	// 恢复阈值（相对值会被难度调整）
	RecoveryLatencyMs *float64 `json:"recovery_latency_ms,omitempty"` // 延迟恢复阈值
	RecoveryErrorRate *float64 `json:"recovery_error_rate,omitempty"` // 错误率恢复阈值
	RecoveryMemoryPct *float64 `json:"recovery_memory_pct,omitempty"` // 内存恢复阈值
}

// DifficultyApplyResult 应用难度后的结果。
type DifficultyApplyResult struct {
	Spec    *FaultSpec             `json:"spec"`     // 调整后的故障参数
	DiffCfg model.DifficultyConfig `json:"diff_cfg"` // 完整难度配置（供前端展示）
}

// DifficultyService 难度参数服务。
type DifficultyService struct {
	// 未来可扩展：从数据库/配置中心加载自定义难度
}

// NewDifficultyService 创建难度参数服务。
func NewDifficultyService() *DifficultyService {
	return &DifficultyService{}
}

// ListDifficulties 返回所有可选难度列表。
func (s *DifficultyService) ListDifficulties(_ context.Context) []model.DifficultyConfig {
	return model.ListAll()
}

// GetDifficulty 获取指定难度配置。
func (s *DifficultyService) GetDifficulty(_ context.Context, level string) (model.DifficultyConfig, error) {
	return model.GetConfig(level)
}

// ApplyToFaultSpec 将难度参数应用到故障剧本配置。
//
// 调整逻辑：
//   - 注入强度 × FaultIntensity 倍率
//   - 恢复阈值反向调整（难度越高，恢复阈值越宽松→判定越难）
//   - 告警灵敏度影响告警数量
func (s *DifficultyService) ApplyToFaultSpec(_ context.Context, spec *FaultSpec, level string) (*DifficultyApplyResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("故障参数不能为空")
	}

	cfg, err := model.GetConfig(level)
	if err != nil {
		return nil, err
	}

	adjusted := *spec // 浅拷贝

	// ——— 注入强度调整 ———
	if adjusted.DelayMillis != nil {
		v := applyIntensityInt(*spec.DelayMillis, cfg.FaultIntensity, 50, 5000)
		adjusted.DelayMillis = &v
	}
	if adjusted.LossPercent != nil {
		v := applyIntensity(*spec.LossPercent, cfg.FaultIntensity, 5, 90)
		adjusted.LossPercent = &v
	}
	if adjusted.MemoryPercent != nil {
		v := applyIntensity(*spec.MemoryPercent, cfg.FaultIntensity, 50, 95)
		adjusted.MemoryPercent = &v
	}
	if adjusted.CPUPercent != nil {
		v := applyIntensityInt(*spec.CPUPercent, cfg.FaultIntensity, 30, 100)
		adjusted.CPUPercent = &v
	}
	if adjusted.DiskFillPercent != nil {
		v := applyIntensity(*spec.DiskFillPercent, cfg.FaultIntensity, 30, 98)
		adjusted.DiskFillPercent = &v
	}

	// ——— 恢复阈值调整 ———
	// 难度越高，恢复阈值越严格（需要恢复得更好才算修好了）
	// 反向倍率：简单模式阈值宽（容易达标），困难模式阈值严（不容易达标）
	recoveryStrictness := 1.0
	switch cfg.Level {
	case model.LevelEasy:
		recoveryStrictness = 1.5 // 阈值 ×1.5，更容易达标
	case model.LevelHard:
		recoveryStrictness = 0.7 // 阈值 ×0.7，更难达标
	}

	if adjusted.RecoveryLatencyMs != nil {
		v := *spec.RecoveryLatencyMs * recoveryStrictness
		adjusted.RecoveryLatencyMs = &v
	}
	if adjusted.RecoveryErrorRate != nil {
		v := *spec.RecoveryErrorRate * recoveryStrictness
		adjusted.RecoveryErrorRate = &v
	}
	if adjusted.RecoveryMemoryPct != nil {
		v := *spec.RecoveryMemoryPct * recoveryStrictness
		adjusted.RecoveryMemoryPct = &v
	}

	return &DifficultyApplyResult{
		Spec:    &adjusted,
		DiffCfg: cfg,
	}, nil
}

// MarshalFaultSpec 将 FaultSpec 序列化为 JSON。
func MarshalFaultSpec(spec *FaultSpec) (string, error) {
	if spec == nil {
		return "", nil
	}
	b, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("序列化故障参数失败: %w", err)
	}
	return string(b), nil
}

// UnmarshalFaultSpec 从 JSON 反序列化 FaultSpec。
func UnmarshalFaultSpec(s string) (*FaultSpec, error) {
	if s == "" {
		return nil, nil
	}
	var spec FaultSpec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		return nil, fmt.Errorf("反序列化故障参数失败: %w", err)
	}
	return &spec, nil
}

// applyIntensity 应用烈度倍率到原始值。
// 结果在 minVal 和 maxVal 之间限制。
func applyIntensity(original float64, intensity float64, minVal, maxVal float64) float64 {
	result := original * intensity
	if result < minVal {
		return minVal
	}
	if result > maxVal {
		return maxVal
	}
	return result
}

// applyIntensityInt 应用烈度倍率（整数版）。
func applyIntensityInt(original int, intensity float64, minVal, maxVal int) int {
	return int(applyIntensity(float64(original), intensity, float64(minVal), float64(maxVal)))
}
