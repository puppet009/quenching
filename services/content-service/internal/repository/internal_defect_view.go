// 服务间内部查询专用的脱敏投影与只读端口。
//
// 微服务拆分后，training / content 等编排服务不再直接 JOIN content_defects 表，
// 统一经 fault-engine 的 /internal/v1 接口取数。接口只暴露编排所需的
// 非答案字段；下列答案/注入配置严禁经内部接口流出：
//
//	root_cause、trigger_config、manifestation_config、
//	fix_config、amplifier_config、freeze_rules
package repository

import "context"

// InternalDefectView 故障剧本脱敏投影（内部接口响应单元）。
type InternalDefectView struct {
	ID              int64  `json:"id"`
	ProjectID       int64  `json:"project_id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	DefectType      string `json:"defect_type"`
	Category        string `json:"category"`
	Difficulty      string `json:"difficulty"`
	Description     string `json:"description"`
	TargetServiceID int64  `json:"target_service_id"`
	// 以下三列为编排/用户面所需的非敏感展示列（不含答案与注入配置）。
	ScoreWeight     float64 `json:"score_weight"`
	EstimatedFixMin int32   `json:"estimated_fix_time"`
	SortOrder       int32   `json:"sort_order"`
	IsActive        bool    `json:"is_active"`
}

// InternalDefectName 仅含 ID/名称的最小搜索结果。
type InternalDefectName struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// InternalDefectReader 故障剧本服务间只读查询端口。
type InternalDefectReader interface {
	// ListByIDs 按 ID 列表批量返回脱敏投影，避免调用方 N+1；
	// 缺失或已软删的 ID 不占结果位，调用方按返回的 ID 自行对齐。
	ListByIDs(ctx context.Context, ids []int64) ([]*InternalDefectView, error)
	// ListActiveByProject 返回项目下启用中的故障目录（跨组编排选故障用）。
	ListActiveByProject(ctx context.Context, projectID int64) ([]*InternalDefectView, error)
	// SearchByName 按名称模糊搜索，供调用方在本地侧做关键词条件对齐。
	SearchByName(ctx context.Context, keyword string) ([]*InternalDefectName, error)
}
