package model

import (
	"encoding/json"
	"fmt"
	cc "github.com/quenching/proto/contentcontract"
)

// PitfallStoredRow ai_pitfall_configs 的一行原始数据（PG 仓储读出，JSONB 块保持原始字节）。
// 加载器负责把三块 JSONB 解析为强类型并校验，仓储层不关心其内部结构。
type PitfallStoredRow = cc.PitfallStoredRow

// ParsePitfallStoredRow 把一行原始数据（含三块 JSONB）解析为强类型 PitfallConfig 并校验。
// 供后台管理接口（T3-18）和测试用；训练启动时走 Loader.LoadByDefects 批量加载。
func ParsePitfallStoredRow(row *PitfallStoredRow) (*PitfallConfig, error) {
	if row == nil {
		return nil, fmt.Errorf("坑点配置行为空")
	}

	cfg := &PitfallConfig{
		ID:              row.ID,
		DefectID:        row.DefectID,
		PitfallType:     row.PitfallType,
		Name:            row.Name,
		Description:     row.Description,
		ErrorContent:    row.ErrorContent,
		ConfidenceLevel: orDefault(row.ConfidenceLevel, ConfidenceRandom),
		CorrectApproach: row.CorrectApproach,
		Severity:        orDefault(row.Severity, SeverityMedium),
		Status:          orDefault(row.Status, StatusActive),
		SortOrder:       row.SortOrder,
		CreatedBy:       row.CreatedBy,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}

	if err := unmarshalBlock(row.ID, "trigger_condition", row.TriggerCondition, &cfg.Trigger); err != nil {
		return nil, err
	}
	if err := unmarshalBlock(row.ID, "recognition_rules", row.RecognitionRules, &cfg.Recognition); err != nil {
		return nil, err
	}
	if err := unmarshalBlock(row.ID, "pitfall_rules", row.PitfallRules, &cfg.PitfallRules); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// IsValidStatus 判断状态值是否合法。
func IsValidStatus(status string) bool {
	return supportedStatus[status]
}

// orDefault 空字符串兜底。
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// unmarshalBlock 解析一个 JSONB 配置块。
func unmarshalBlock(configID int64, field string, raw []byte, dst any) error {
	if len(raw) == 0 {
		return fmt.Errorf("坑点配置 %d 缺少 %s", configID, field)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("坑点配置 %d 的 %s JSON 非法: %w", configID, field, err)
	}
	return nil
}
