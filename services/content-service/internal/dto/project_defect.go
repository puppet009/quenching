// 用户面项目故障只读 DTO：仅暴露实训前需要了解的非敏感字段，
// 根因、注入/放大器/冻结等运维配置一律不下发。
package dto

// ProjectDefectItem 项目故障概览（用户面）。
type ProjectDefectItem struct {
	ID              int64   `json:"id"`
	ProjectID       int64   `json:"project_id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	DefectType      string  `json:"defect_type"`
	Category        string  `json:"category"`
	Difficulty      string  `json:"difficulty"`
	Description     string  `json:"description"`
	TargetServiceID *int64  `json:"target_service_id"`
	ScoreWeight     float64 `json:"score_weight"`
	EstimatedFixMin int32   `json:"estimated_fix_time"`
	SortOrder       int32   `json:"sort_order"`
}
