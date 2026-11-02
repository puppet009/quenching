// 评分体系模型（T2-6）。
//
// 5 大评分维度：
//   - 排障速度（30%）：故障发生到恢复的时间
//   - 恢复质量（25%）：恢复验证得分
//   - 方法合理性（20%）：操作步骤是否合理
//   - 知识掌握（15%）：答题/选择正确性
//   - 团队协作（10%）：协作模式下的配合度
package model

// ========== 评分维度 ==========

// Dimension 评分维度。
type Dimension string

const (
	DimSpeed         Dimension = "speed"         // 排障速度
	DimRecovery      Dimension = "recovery"      // 恢复质量
	DimMethod        Dimension = "method"        // 方法合理性
	DimKnowledge     Dimension = "knowledge"     // 知识掌握
	DimCollaboration Dimension = "collaboration" // 团队协作
)

// DimensionConfig 维度配置。
type DimensionConfig struct {
	Dimension   Dimension `json:"dimension"`   // 维度
	Name        string    `json:"name"`        // 维度名称
	Description string    `json:"description"` // 描述
	Weight      float64   `json:"weight"`      // 权重（0-1，总和 1.0）
	MaxScore    float64   `json:"max_score"`   // 该维度满分
}

// ========== 评分等级 ==========

// Grade 评分等级。
type Grade string

const (
	GradeExcellent Grade = "excellent" // 优秀 (90-100)
	GradeGood      Grade = "good"      // 良好 (80-89)
	GradePass      Grade = "pass"      // 及格 (60-79)
	GradeFail      Grade = "fail"      // 不及格 (0-59)
)

// ScoringGradeInfo 等级信息。
type ScoringGradeInfo struct {
	Grade       Grade   `json:"grade"`
	Name        string  `json:"name"`
	MinScore    float64 `json:"min_score"`
	MaxScore    float64 `json:"max_score"`
	Color       string  `json:"color"`
	Description string  `json:"description"`
}

// ========== 维度得分 ==========

// DimensionScore 单个维度的得分。
type DimensionScore struct {
	Dimension  Dimension   `json:"dimension"`
	Name       string      `json:"name"`
	Weight     float64     `json:"weight"`
	Score      float64     `json:"score"`    // 原始得分（0-100）
	Weighted   float64     `json:"weighted"` // 加权得分（score * weight）
	MaxScore   float64     `json:"max_score"`
	Feedback   string      `json:"feedback"`   // 评价反馈
	Deductions []Deduction `json:"deductions"` // 扣分明细
}

// Deduction 扣分项。
type Deduction struct {
	Reason string  `json:"reason"` // 扣分原因
	Points float64 `json:"points"` // 扣分数
}

// ========== 评分结果 ==========

// ScoreResult 评分结果。
type ScoreResult struct {
	SessionID    string           `json:"session_id"`    // 训练会话ID
	UserID       int64            `json:"user_id"`       // 用户ID
	TotalScore   float64          `json:"total_score"`   // 总分（0-100）
	Grade        ScoringGradeInfo `json:"grade"`         // 等级
	Dimensions   []DimensionScore `json:"dimensions"`    // 各维度得分
	OverallTime  int64            `json:"overall_time"`  // 总用时（秒）
	RecoveryTime int64            `json:"recovery_time"` // 恢复用时（秒）
	ScoredAt     int64            `json:"scored_at"`     // 评分时间
	Feedback     string           `json:"feedback"`      // 总体评价
}

// ========== 评分输入 ==========

// ScoreInput 评分输入数据。
type ScoreInput struct {
	SessionID       string  // 会话ID
	UserID          int64   // 用户ID
	FaultStartTime  int64   // 故障发生时间（毫秒）
	RecoveryTime    int64   // 恢复时间（毫秒，0=未恢复）
	RecoveryScore   float64 // 恢复验证得分（0-100）
	MethodScore     float64 // 方法得分（0-100）
	KnowledgeScore  float64 // 知识得分（0-100）
	CollabScore     float64 // 协作得分（0-100）
	ExpectedSeconds int     // 预期完成时间（秒），用于速度评分
}
