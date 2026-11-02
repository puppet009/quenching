// 用户能力档案（user_profiles 行）：训练过程中系统计算，注册时初始化。
package model

import "encoding/json"

// UserProfile 用户协作/技术能力档案。
type UserProfile struct {
	UserID         int64           `json:"user_id"`
	CollabLevel    string          `json:"collab_level"`     // C1..C5
	CollabGroup    string          `json:"collab_group"`     // A/B，首训前可能为空
	AvgCollabScore *float64        `json:"avg_collab_score"` // 平均协作分
	AvgScore       *float64        `json:"avg_score"`        // 平均技术分
	CollabRadar    json.RawMessage `json:"collab_radar"`     // 协作雷达（无数据时为 null）
	SkillRadar     json.RawMessage `json:"skill_radar"`      // 技术雷达（无数据时为 null）
}
