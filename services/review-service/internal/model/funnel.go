package model

import "time"

// Funnel 漏斗定义。
// 一个漏斗由有序的事件步骤组成，用于分析用户转化路径。
type Funnel struct {
	Code        string   `json:"code"`        // 漏斗唯一编码
	Name        string   `json:"name"`        // 漏斗名称
	Description string   `json:"description"` // 描述
	Steps       []string `json:"steps"`       // 漏斗步骤（事件类型，按顺序）
}

// FunnelStepResult 漏斗单步结果。
type FunnelStepResult struct {
	StepIndex  int     `json:"step_index"` // 步骤序号（从 0 开始）
	EventType  string  `json:"event_type"` // 事件类型
	UserCount  int     `json:"user_count"` // 到达该步骤的用户数
	Conversion float64 `json:"conversion"` // 相对上一步的转化率（0-100%）
	DropOff    float64 `json:"drop_off"`   // 相对上一步的流失率（0-100%）
}

// FunnelResult 漏斗分析结果。
type FunnelResult struct {
	FunnelCode        string             `json:"funnel_code"`
	FunnelName        string             `json:"funnel_name"`
	Steps             []FunnelStepResult `json:"steps"`
	TotalUsers        int                `json:"total_users"`        // 第一步用户数
	FinalUsers        int                `json:"final_users"`        // 最后一步用户数
	OverallConversion float64            `json:"overall_conversion"` // 整体转化率（%）

	// 时间范围
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}

// 预定义漏斗。

// TrainingFunnel 训练漏斗（MVP 核心漏斗）。
// 注册 → 首次启动 → 首次完成 → 复训
var TrainingFunnel = Funnel{
	Code:        "training_funnel",
	Name:        "训练转化漏斗",
	Description: "用户从注册到复训的完整转化路径",
	Steps: []string{
		EventRegister,
		EventFirstStart,
		EventFirstComplete,
		EventRetrain,
	},
}

// BuiltInFunnels 所有内置漏斗。
var BuiltInFunnels = []Funnel{
	TrainingFunnel,
}

// GetFunnel 按编码获取漏斗定义。
func GetFunnel(code string) (*Funnel, bool) {
	for _, f := range BuiltInFunnels {
		if f.Code == code {
			clone := f
			return &clone, true
		}
	}
	return nil, false
}
