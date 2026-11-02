package model

import (
	"encoding/json"
	"fmt"
)

// Parse 从 ai_pitfall_configs 的 JSONB 行解析一条坑点配置并校验。
// raw 为整行配置的 JSON（含基础字段与三个 JSONB 配置块）。
func Parse(raw []byte) (*PitfallConfig, error) {
	var c PitfallConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("坑点配置 JSON 解析失败: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// MustParseJSON 便于测试/种子数据，失败 panic。
func MustParseJSON(raw string) *PitfallConfig {
	c, err := Parse([]byte(raw))
	if err != nil {
		panic(err)
	}
	return c
}

// Validate 校验一条坑点配置的必填项与取值合法性。
// 加载阶段要保证 T3-9 拿到的每条配置都自洽，否则训练中途才暴露会更难排查。
func (c *PitfallConfig) Validate() error {
	if c.ID <= 0 {
		return fmt.Errorf("坑点配置缺少 id")
	}
	if c.DefectID <= 0 {
		return fmt.Errorf("坑点配置 %d 缺少 defect_id", c.ID)
	}
	if !supportedTypes[c.PitfallType] {
		return fmt.Errorf("坑点配置 %d 非法 pitfall_type: %q", c.ID, c.PitfallType)
	}
	if c.Name == "" {
		return fmt.Errorf("坑点配置 %d 缺少 name", c.ID)
	}
	if c.ErrorContent == "" {
		return fmt.Errorf("坑点配置 %d 缺少 error_content（错误建议模板）", c.ID)
	}
	if c.Status != "" && !supportedStatus[c.Status] {
		return fmt.Errorf("坑点配置 %d 非法 status: %q", c.ID, c.Status)
	}
	if c.ConfidenceLevel != "" && !supportedConfidence[c.ConfidenceLevel] {
		return fmt.Errorf("坑点配置 %d 非法 confidence_level: %q", c.ID, c.ConfidenceLevel)
	}
	if c.Severity != "" && !supportedSeverity[c.Severity] {
		return fmt.Errorf("坑点配置 %d 非法 severity: %q", c.ID, c.Severity)
	}
	if err := c.Trigger.Validate(c.ID); err != nil {
		return err
	}
	if err := c.Recognition.Validate(c.ID); err != nil {
		return err
	}
	if err := c.PitfallRules.Validate(c.ID); err != nil {
		return err
	}
	return nil
}

// Validate 触发条件合法性。
func (t TriggerCondition) Validate(configID int64) error {
	when := t.EffectiveWhen()
	if !supportedWhen[when] {
		return fmt.Errorf("坑点配置 %d 非法触发时机 when: %q", configID, t.When)
	}
	if t.Probability < 0 || t.Probability > 1 {
		return fmt.Errorf("坑点配置 %d probability 必须在 [0,1]", configID)
	}
	if t.MaxTimes < 0 {
		return fmt.Errorf("坑点配置 %d max_times 不能为负", configID)
	}

	switch when {
	case WhenQuestionMatch:
		if len(t.QuestionKeywords) == 0 && len(t.QuestionPatterns) == 0 {
			return fmt.Errorf("坑点配置 %d question_match 触发至少需要一个关键词或正则模式", configID)
		}
	case WhenStageEnter:
		if len(t.Stages) == 0 {
			return fmt.Errorf("坑点配置 %d stage_enter 触发至少需要一个阶段", configID)
		}
	case WhenDelay:
		if t.Window != nil {
			if t.Window.StartSeconds < 0 || t.Window.EndSeconds <= t.Window.StartSeconds {
				return fmt.Errorf("坑点配置 %d 时间窗口非法", configID)
			}
		} else if t.DelaySeconds < 0 {
			return fmt.Errorf("坑点配置 %d delay_seconds 不能为负", configID)
		}
	}

	if t.Requires != nil {
		if t.Requires.MinTurn < 0 || t.Requires.MaxTurn < 0 {
			return fmt.Errorf("坑点配置 %d 前置轮次不能为负", configID)
		}
		if t.Requires.MaxTurn != 0 && t.Requires.MaxTurn < t.Requires.MinTurn {
			return fmt.Errorf("坑点配置 %d max_turn 不能小于 min_turn", configID)
		}
	}
	return nil
}

// Validate 识别规则：至少声明一种"识别了坑"的判定，否则该坑永远无法被判定为识别。
func (r RecognitionRules) Validate(configID int64) error {
	if len(r.DisputeKeywords) == 0 && len(r.IndependentVerify) == 0 && !r.IgnoreAndProceed {
		return fmt.Errorf("坑点配置 %d recognition_rules 至少需要一种识别判定", configID)
	}
	return nil
}

// Validate 踩坑规则：至少声明一种"踩了坑"的判定。
func (f FallRules) Validate(configID int64) error {
	if len(f.AdoptActions) == 0 && len(f.AdoptKeywords) == 0 && !f.WorsenFault {
		return fmt.Errorf("坑点配置 %d pitfall_rules 至少需要一种踩坑判定", configID)
	}
	// T3-12：级联校验次生故障规格（nil 接收者直接放行）。
	if err := f.Consequence.Validate(configID); err != nil {
		return err
	}
	return nil
}
