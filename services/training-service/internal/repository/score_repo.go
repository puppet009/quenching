// 评分相关仓储（T3-24 协作指数汇总）。
//
// 实现编排层要求的 service.ScoreStore 端口：
//   - 从 ai_messages / ai_conversations / training_operation_projection 收集协作行为数据
//   - 回写协作分到 training_sessions（本服务属主表）
//   - 经 review-service 内部接口回写协作子分到 reviews
//   - 经 user-service 内部接口提交协作雷达、等级与平均分
//
// 协作滚动平均由 user-service 在档案属主侧计算；本仓储不直接访问 user_profiles
// 或 reviews。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// 编译期保证实现编排层接口。
var _ service.ScoreStore = (*ScoreRepo)(nil)

// ScoreRepo 评分数据仓储。
type ScoreRepo struct {
	waitOperations func(context.Context, int64) error
	pool           *pgxpool.Pool
	users          service.UserProfileStore
	reviews        service.ReviewStore
}

func (r *ScoreRepo) WithOperationReadiness(check func(context.Context, int64) error) *ScoreRepo {
	r.waitOperations = check
	return r
}
func (r *ScoreRepo) WaitForOperations(ctx context.Context, sid int64) error {
	if r.waitOperations == nil {
		return fmt.Errorf("source operation checkpoint unavailable")
	}
	return r.waitOperations(ctx, sid)
}
func (r *ScoreRepo) ListUnscoredCompleted(ctx context.Context) ([]int64, error) {
	rows, e := r.pool.Query(ctx, `SELECT id FROM training_sessions WHERE status='completed' AND fault_resolved=true AND tech_score IS NULL AND deleted_at IS NULL AND ended_at<now()-interval '30 seconds' ORDER BY id LIMIT 20`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// NewScoreRepo 创建评分仓储。users 为 user-service 用户档案端口，
// reviews 为 review-service 复盘协作数据端口（两者均可为 nil，对应懒降级）。
func NewScoreRepo(
	pool *pgxpool.Pool, users service.UserProfileStore, reviews service.ReviewStore,
) *ScoreRepo {
	return &ScoreRepo{pool: pool, users: users, reviews: reviews}
}

// ——— 协作行为数据采集 ———

// CollectCollabInput 从数据库收集一次训练会话的协作行为事件，
// 交由编排层发往 ai-service 评估（本服务不再本地计算协作子分）。
func (r *ScoreRepo) CollectCollabInput(ctx context.Context, sessionID int64) (service.CollabEvents, error) {
	var in service.CollabEvents

	questions, err := r.collectQuestions(ctx, sessionID)
	if err != nil {
		return in, err
	}
	in.Questions = questions

	pitfalls, err := r.collectPitfallOutcomes(ctx, sessionID)
	if err != nil {
		return in, err
	}
	in.Pitfalls = pitfalls

	actions, err := r.collectActions(ctx, sessionID)
	if err != nil {
		if isUndefinedTableErr(err) {
			actions = nil
		} else {
			return in, err
		}
	}
	in.Actions = actions

	return in, nil
}

func (r *ScoreRepo) collectQuestions(ctx context.Context, sessionID int64) ([]service.CollabQuestionEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.content
		  FROM ai_messages m
		  JOIN ai_conversations c ON c.id = m.conversation_id
		 WHERE c.session_id = $1
		   AND c.conversation_type = 'training_assistant'
		   AND m.role = 'user'
		 ORDER BY m.id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var qs []service.CollabQuestionEvent
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, err
		}
		qs = append(qs, service.CollabQuestionEvent{Text: text})
	}
	return qs, rows.Err()
}

func (r *ScoreRepo) collectPitfallOutcomes(ctx context.Context, sessionID int64) ([]service.CollabPitfallEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.user_response_type
		  FROM ai_messages m
		  JOIN ai_conversations c ON c.id = m.conversation_id
		 WHERE c.session_id = $1
		   AND m.is_pitfall = true
		   AND m.user_response_type IS NOT NULL
		 ORDER BY m.id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ps []service.CollabPitfallEvent
	for rows.Next() {
		var respType string
		if err := rows.Scan(&respType); err != nil {
			return nil, err
		}
		ps = append(ps, service.CollabPitfallEvent{Outcome: mapResponseToOutcome(respType)})
	}
	return ps, rows.Err()
}

func (r *ScoreRepo) collectActions(ctx context.Context, sessionID int64) ([]service.CollabActionEvent, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			 WHERE table_name = 'training_operation_projection'
		)
	`).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT operation_type, detail
		  FROM training_operation_projection
		 WHERE session_id = $1
		 ORDER BY id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []service.CollabActionEvent
	for rows.Next() {
		var opType string
		var detail *string
		if err := rows.Scan(&opType, &detail); err != nil {
			return nil, err
		}
		kind := service.CollabActionIndependent
		if opType == "follow_ai" {
			kind = service.CollabActionFollowAI
		}
		verified := false
		if detail != nil {
			d := *detail
			if containsAnyLower(d, []string{"验证", "先查", "确认", "检查", "verify", "check"}) {
				verified = true
			}
		}
		actions = append(actions, service.CollabActionEvent{
			Kind:                 kind,
			VerifiedBeforeFollow: verified,
		})
	}
	return actions, rows.Err()
}

// ——— 评分回写 ———

func (r *ScoreRepo) UpdateSessionCollabScore(ctx context.Context, sessionID int64, collabScore float64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE training_sessions
		   SET collab_score = $1, updated_at = now()
		 WHERE id = $2
	`, collabScore, sessionID)
	return err
}

// UpdateReviewCollabScores 经 review-service 内部接口回写协作子分。
// 远端不可用或未配置时返回错误，编排层本就忽略该错误（尽力而为）。
func (r *ScoreRepo) UpdateReviewCollabScores(ctx context.Context, sessionID int64, d service.ReviewCollabDetail) error {
	if r.reviews == nil {
		return nil
	}
	_, err := r.reviews.SubmitCollabScores(ctx, sessionID, d)
	return err
}

func (r *ScoreRepo) UpdateUserProfileCollab(
	ctx context.Context,
	userID int64,
	collabRadar model.RadarData,
	collabScore float64,
	level string,
) error {
	if r.users == nil {
		return nil
	}
	return r.users.SubmitCollabRollup(ctx, userID, collabRadar, collabScore, level)
}

// GetSessionScoreDetail 查询会话评分详情。
func (r *ScoreRepo) GetSessionScoreDetail(
	ctx context.Context,
	sessionID, userID int64,
) (*service.SessionScoreResult, error) {
	result := &service.SessionScoreResult{SessionID: sessionID}

	var techScore, collabScore *float64
	var faultResolved bool
	var difficulty string

	err := r.pool.QueryRow(ctx, `
		SELECT tech_score, collab_score, fault_resolved, difficulty
		  FROM training_sessions
		 WHERE id = $1 AND user_id = $2
	`, sessionID, userID).Scan(&techScore, &collabScore, &faultResolved, &difficulty)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	result.FaultResolved = faultResolved
	result.Difficulty = difficulty
	if techScore != nil {
		result.TechScore = *techScore
		result.HasTechScore = true
	}
	if collabScore != nil {
		result.CollabScore = *collabScore
		result.HasCollabScore = true
	}

	// 经 review-service 读取 reviews 协作明细；保持原共库口径：任何失败
	// （含未配置 / 远端错误 / 复盘不存在）都静默，仅回填成功取得的字段。
	if r.reviews != nil {
		rv, rvErr := r.reviews.GetReviewBySession(ctx, sessionID)
		if rvErr == nil && rv != nil {
			if rv.QuestionQuality != nil {
				result.QuestionQuality = *rv.QuestionQuality
			}
			if rv.InfoUtilization != nil {
				result.InfoUtilization = *rv.InfoUtilization
			}
			if rv.Judgment != nil {
				result.Judgment = *rv.Judgment
			}
			if rv.ErrorRecognitionRate != nil {
				result.ErrorRecognitionRate = *rv.ErrorRecognitionRate
			}
			if rv.EvidenceValidationRate != nil {
				result.EvidenceValidationRate = *rv.EvidenceValidationRate
			}
			if rv.PitfallCount != nil {
				result.PitfallCount = *rv.PitfallCount
			}
			if rv.PitfallRecognized != nil {
				result.PitfallRecognized = *rv.PitfallRecognized
			}
		}
	}

	// 经 user-service 读取历史协作雷达、均分和等级；远端失败时保留本地会话分。
	if r.users != nil {
		if profile, profileErr := r.users.GetCollabProfile(ctx, userID); profileErr == nil && profile != nil {
			result.AvgCollabRadar = profile.Radar
			if profile.AvgScore != nil {
				result.AvgCollabScore = *profile.AvgScore
			}
			result.CollabLevel = profile.Level
		}
	}

	return result, nil
}

// ——— runtime finish 最小闭环（i3） ———

// GetSessionFinishSnapshot 读取会话结束评分快照。
// tech_score 可空，用 *float64 承接；其余列在建表迁移中保证非空或本就可空（actual_duration/started_at/ended_at）。
func (r *ScoreRepo) GetSessionFinishSnapshot(
	ctx context.Context,
	sessionID int64,
) (*service.SessionFinishSnapshot, error) {
	var (
		snap      service.SessionFinishSnapshot
		techScore *float64
		actualDur *int
		startedAt *time.Time
		endedAt   *time.Time
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, status, difficulty, actual_duration,
		       started_at, ended_at, total_defect_count, fixed_defect_count,
		       tech_score, fault_resolved
		  FROM training_sessions
		 WHERE id = $1 AND deleted_at IS NULL
	`, sessionID).Scan(
		&snap.SessionID, &snap.UserID, &snap.Status, &snap.Difficulty, &actualDur,
		&startedAt, &endedAt, &snap.TotalDefectCount, &snap.FixedDefectCount,
		&techScore, &snap.FaultResolved,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	snap.ActualDuration = actualDur
	snap.StartedAt = startedAt
	snap.EndedAt = endedAt
	snap.TechScoreSet = techScore != nil
	return &snap, nil
}

// ListSessionOperations 读取会话操作时间线并映射为评分用 OperationAction。
// 与 collectActions 一致，先探测表是否存在（T1-23 采集链路未部署时降级为空）。
// virtual_timestamp 为相对开始的秒数，评分时间戳量纲为毫秒，故乘 1000。
func (r *ScoreRepo) ListSessionOperations(
	ctx context.Context,
	sessionID int64,
) ([]model.OperationAction, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			 WHERE table_name = 'training_operation_projection'
		)
	`).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT operation_type, COALESCE(virtual_timestamp, 0)
		  FROM training_operation_projection
		 WHERE session_id = $1 AND deleted_at IS NULL
		 ORDER BY id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	actions := make([]model.OperationAction, 0)
	for rows.Next() {
		var opType string
		var virtualSec int
		if err := rows.Scan(&opType, &virtualSec); err != nil {
			return nil, err
		}
		actions = append(actions, model.OperationAction{
			Type:        mapOperationType(opType),
			TimestampMs: int64(virtualSec) * 1000,
			// CalcCostPenalty 当前只按类型基础代价/重复/盲动计分，不消费有效性标记；
			// 标记统一置 true，待操作结果（result 列）纳入规则时再细化。
			IsEffective: true,
		})
	}
	return actions, rows.Err()
}

// mapOperationType 把 training_operation_projection.operation_type 映射为评分操作类型。
// 建表注释取值与评分常量基本同名；ask_ai 为表侧写法，归一到 ai_question；
// follow_ai 是协作事件语义而非操作类型，最小闭环按 AI 提问计入代价；未知归 other。
func mapOperationType(opType string) model.OperationType {
	switch model.OperationType(opType) {
	case model.OpViewMetric,
		model.OpViewLog,
		model.OpViewConfig,
		model.OpViewAlert,
		model.OpExecCommand,
		model.OpModifyConfig,
		model.OpScaleResource,
		model.OpRestartService,
		model.OpAIQuestion,
		model.OpFixComplete:
		return model.OperationType(opType)
	}
	switch opType {
	case "ask_ai", "follow_ai":
		return model.OpAIQuestion
	default:
		return model.OpOther
	}
}

// UpdateSessionTechScore 回写技术分、技术雷达与修复结论。
// tech_radar 为 JSONB 列，序列化 model.RadarData 后写入；序列化失败返回错误不落库。
func (r *ScoreRepo) UpdateSessionTechScore(
	ctx context.Context,
	sessionID int64,
	techScore float64,
	techRadar model.RadarData,
	faultResolved bool,
) error {
	radarJSON, err := json.Marshal(techRadar)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
        UPDATE training_sessions
           SET tech_score = $1,
               tech_radar = $2,
               fault_resolved = $3,
               updated_at = now()
         WHERE id = $4
    `, techScore, radarJSON, faultResolved, sessionID)
	return err
}

// ——— 工具函数 ———

func mapResponseToOutcome(respType string) service.PitfallOutcome {
	switch respType {
	case "dispute", "verified":
		return service.PitfallOutcomeRecognized
	case "agree", "fell":
		return service.PitfallOutcomeFell
	default:
		return service.PitfallOutcomePending
	}
}

func isUndefinedTableErr(err error) bool {
	if err == nil {
		return false
	}
	return containsAnyLower(err.Error(), []string{"undefined_table", "does not exist", "relation"})
}

func containsAnyLower(s string, kws []string) bool {
	// Go 1.23+ 有 strings.ToLower + strings.Contains；这里手写避免额外 import
	for _, k := range kws {
		if k == "" {
			continue
		}
		if len(s) < len(k) {
			continue
		}
		// 简单不区分大小写子串匹配
		kl := len(k)
		for i := 0; i <= len(s)-kl; i++ {
			if caseInsensitiveEqual(s[i:i+kl], k) {
				return true
			}
		}
	}
	return false
}

func caseInsensitiveEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ai := a[i]
		bi := b[i]
		if ai >= 'A' && ai <= 'Z' {
			ai += 32
		}
		if bi >= 'A' && bi <= 'Z' {
			bi += 32
		}
		if ai != bi {
			return false
		}
	}
	return true
}
