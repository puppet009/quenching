// 对照数据统计（T3-26）的 PG 仓储。
//
// 查询 training_sessions 中已配对的会话对，计算 AI vs 人工的四项差异。
// 数据来源：
//   - 基础信息：training_sessions（control_group, pair_session_id, ai_mode, actual_duration 等）
//   - 危险操作：training_operation_projection（penalty > 0 的操作计数）
//   - 验证行为：evidence_validation_rate 字段 + pitfall_recognized
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// ABStatsRepo 对照统计 PG 仓储。
type ABStatsRepo struct {
	pool *pgxpool.Pool
}

// NewABStatsRepo 创建对照统计仓储。
func NewABStatsRepo(pool *pgxpool.Pool) *ABStatsRepo {
	return &ABStatsRepo{pool: pool}
}

// 编译期保证实现 ABStatsPairStore 接口。
var _ service.ABStatsPairStore = (*ABStatsRepo)(nil)

// pairQuery 查询已配对会话对的基础 SQL。
// 通过自连接把两个会话的数据拼成一行，用 s1.id < s2.id 避免重复。
const pairQuery = `
SELECT
    s1.user_id,
    COALESCE(s1.project_id, 0),
    s1.control_group,
    s1.difficulty,
    -- AI 侧
    CASE WHEN s1.ai_mode = 'assistant' THEN s1.id ELSE s2.id END AS ai_session_id,
    CASE WHEN s1.ai_mode = 'assistant' THEN s1.actual_duration ELSE s2.actual_duration END AS ai_duration,
    CASE WHEN s1.ai_mode = 'assistant' THEN s1.fixed_defect_count > 0 ELSE s2.fixed_defect_count > 0 END AS ai_resolved,
    CASE WHEN s1.ai_mode = 'assistant' THEN COALESCE(s1.total_score, 0) ELSE COALESCE(s2.total_score, 0) END AS ai_tech_score,
    CASE WHEN s1.ai_mode = 'assistant' THEN COALESCE(s1.penalty_score, 0) ELSE COALESCE(s2.penalty_score, 0) END AS ai_cost_penalty,
    CASE WHEN s1.ai_mode = 'assistant' THEN COALESCE(s1.pitfall_recognized, 0) ELSE COALESCE(s2.pitfall_recognized, 0) END AS ai_pitfall_recognized,
    -- 人工侧
    CASE WHEN s1.ai_mode = 'none' THEN s1.id ELSE s2.id END AS human_session_id,
    CASE WHEN s1.ai_mode = 'none' THEN s1.actual_duration ELSE s2.actual_duration END AS human_duration,
    CASE WHEN s1.ai_mode = 'none' THEN s1.fixed_defect_count > 0 ELSE s2.fixed_defect_count > 0 END AS human_resolved,
    CASE WHEN s1.ai_mode = 'none' THEN COALESCE(s1.total_score, 0) ELSE COALESCE(s2.total_score, 0) END AS human_tech_score,
    CASE WHEN s1.ai_mode = 'none' THEN COALESCE(s1.penalty_score, 0) ELSE COALESCE(s2.penalty_score, 0) END AS human_cost_penalty,
    CASE WHEN s1.ai_mode = 'none' THEN COALESCE(s1.pitfall_recognized, 0) ELSE COALESCE(s2.pitfall_recognized, 0) END AS human_pitfall_recognized,
    -- 完成时间（取较晚的）
    GREATEST(s1.ended_at, s2.ended_at) AS completed_at
FROM training_sessions s1
JOIN training_sessions s2 ON s1.pair_session_id = s2.id
WHERE s1.id < s2.id
  AND s1.deleted_at IS NULL
  AND s2.deleted_at IS NULL
  AND s1.status IN ('completed', 'resolved')
  AND s2.status IN ('completed', 'resolved')
  AND s1.control_group IN ('A', 'B')
  AND s2.control_group IN ('A', 'B')
`

// ListPairs 列出满足筛选条件的所有对照配对。
func (r *ABStatsRepo) ListPairs(ctx context.Context, filter model.ABFilter) ([]model.ABPair, error) {
	// 构建动态 WHERE
	query := pairQuery
	args := make([]any, 0)
	argIdx := 1

	conditions := []string{}

	if filter.ProjectID > 0 {
		conditions = append(conditions, "s1.project_id = $"+itoa(argIdx))
		args = append(args, filter.ProjectID)
		argIdx++
	}

	if filter.Difficulty != "" {
		conditions = append(conditions, "s1.difficulty = $"+itoa(argIdx))
		args = append(args, filter.Difficulty)
		argIdx++
	}

	if !filter.StartDate.IsZero() {
		conditions = append(conditions, "GREATEST(s1.ended_at, s2.ended_at) >= $"+itoa(argIdx))
		args = append(args, filter.StartDate)
		argIdx++
	}

	if !filter.EndDate.IsZero() {
		conditions = append(conditions, "GREATEST(s1.ended_at, s2.ended_at) < $"+itoa(argIdx))
		args = append(args, filter.EndDate)
		argIdx++
	}

	if len(conditions) > 0 {
		// 在原查询的 WHERE 后追加
		query += " AND " + joinAnd(conditions)
	}

	query += " ORDER BY completed_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pairs := make([]model.ABPair, 0)

	for rows.Next() {
		var p model.ABPair
		var aiDuration, humanDuration *int
		var aiResolved, humanResolved bool
		var aiTechScore, humanTechScore, aiCostPenalty, humanCostPenalty float64
		var aiPitfallRec, humanPitfallRec int
		var completedAt *time.Time

		err := rows.Scan(
			&p.UserID, &p.ProjectID, &p.Group, &p.Difficulty,
			&p.AI.SessionID, &aiDuration, &aiResolved, &aiTechScore, &aiCostPenalty, &aiPitfallRec,
			&p.Human.SessionID, &humanDuration, &humanResolved, &humanTechScore, &humanCostPenalty, &humanPitfallRec,
			&completedAt,
		)
		if err != nil {
			return nil, err
		}

		if aiDuration != nil {
			p.AI.DurationSec = *aiDuration
		}
		if humanDuration != nil {
			p.Human.DurationSec = *humanDuration
		}

		p.AI.Resolved = aiResolved
		p.Human.Resolved = humanResolved
		p.AI.TechScore = aiTechScore
		p.Human.TechScore = humanTechScore
		p.AI.CostPenalty = aiCostPenalty
		p.Human.CostPenalty = humanCostPenalty

		// 验证次数：用 pitfall_recognized 作为近似（识别坑点的次数≈验证行为）
		p.AI.VerifyCount = aiPitfallRec
		p.Human.VerifyCount = humanPitfallRec

		// 危险操作数：用惩罚分 / 10 近似（每次危险操作约 10 分惩罚）
		p.AI.DangerousOps = int(aiCostPenalty / 10)
		p.Human.DangerousOps = int(humanCostPenalty / 10)

		// 提示次数：暂时用 0 占位，后续从 training_operation_projection 统计
		p.AI.HintCount = 0
		p.Human.HintCount = 0

		if completedAt != nil {
			p.CompletedAt = *completedAt
		}

		// Category 暂时留空（需要联 defects 表，MVP 先不做）
		p.Category = ""

		pairs = append(pairs, p)
	}

	return pairs, rows.Err()
}

// ——— 工具函数 ———

func itoa(n int) string {
	return itoaBytes(n)
}

func itoaBytes(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func joinAnd(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	result := conds[0]
	for i := 1; i < len(conds); i++ {
		result += " AND " + conds[i]
	}
	return result
}
