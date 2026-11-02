package contentcontract

import "time"

// Defect 故障剧本（defects 表）。
// 一个故障剧本包含完整的注入配置、触发条件、表现特征、修复验证等。
type Defect struct {
	Provenance       JSONMap           `json:"provenance"`
	TemplateBindings []TemplateBinding `json:"template_bindings"`
	ID               int64             `json:"id"`
	ProjectID        int64             `json:"project_id"`
	Code             string            `json:"code"`
	Name             string            `json:"name"`
	DefectType       string            `json:"defect_type"` // code/config/architecture
	Category         string            `json:"category"`    // database/cache/mq/network/cpu/memory...
	Difficulty       string            `json:"difficulty"`  // easy/medium/hard
	Description      string            `json:"description"`
	RootCause        string            `json:"root_cause"`
	TargetServiceID  int64             `json:"target_service_id"`

	// 核心配置（JSONB）
	TriggerConfig       JSONMap `json:"trigger_config"`       // 触发条件
	ManifestationConfig JSONMap `json:"manifestation_config"` // 表现配置（指标/日志/告警）
	FixConfig           JSONMap `json:"fix_config"`           // 修复验证
	AmplifierConfig     JSONMap `json:"amplifier_config"`     // 缺陷放大器（ChaosBlade）
	FreezeRules         JSONMap `json:"freeze_rules"`         // 弹性冻结规则

	ScoreWeight      float64 `json:"score_weight"`
	EstimatedFixTime int     `json:"estimated_fix_time"` // 分钟
	IsActive         bool    `json:"is_active"`
	SortOrder        int     `json:"sort_order"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JSONMap 通用 JSONB map 类型。
type JSONMap map[string]interface{}

type TemplateBinding struct {
	Executor       string  `json:"executor"`
	TemplateSlug   string  `json:"template_slug"`
	TargetWorkload string  `json:"target_workload"`
	Status         string  `json:"status"`
	Overrides      JSONMap `json:"overrides"`
	Evidence       JSONMap `json:"evidence"`
}
