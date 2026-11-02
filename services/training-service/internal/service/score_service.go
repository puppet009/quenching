// 评分汇总服务（T3-24 协作指数汇总）。
//
// 职责：
//   - 从自有三表收集协作行为事件（ai_messages / ai_conversations / session_operations）
//   - 调用 ai-service EvaluateCollab 取得三维子分/两率/计数（确定性规则在 ai-service）
//   - 加权合并为协作总分、构造协作维雷达
//   - 回写评分到 training_sessions / reviews，协作档案经 user-service 内部接口更新
//   - 提供会话评分查询能力
//
// 纯编排：不直接操作 DB（数据采集与回写通过 ScoreStore 端口），
// 不在本服务实现任何协作评分规则（规则唯一实现在 ai-service）。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/quenching/proto/aiv1"

	"github.com/quenching/training-service/internal/model"
)

// ScoreStore 评分数据端口（PG 实现在 repository.ScoreRepo）。
type ScoreStore interface {
	// CollectCollabInput 收集一次会话的协作行为事件。
	CollectCollabInput(ctx context.Context, sessionID int64) (CollabEvents, error)
	// UpdateSessionCollabScore 回写协作分到 training_sessions。
	UpdateSessionCollabScore(ctx context.Context, sessionID int64, collabScore float64) error
	// UpdateReviewCollabScores 回写协作明细到 reviews。
	UpdateReviewCollabScores(ctx context.Context, sessionID int64, detail ReviewCollabDetail) error
	// UpdateUserProfileCollab 更新用户档案的协作维数据。
	UpdateUserProfileCollab(
		ctx context.Context,
		userID int64,
		collabRadar model.RadarData,
		collabScore float64,
		level string,
	) error
	// GetSessionScoreDetail 查询会话评分详情。
	GetSessionScoreDetail(ctx context.Context, sessionID, userID int64) (*SessionScoreResult, error)
	// GetSessionFinishSnapshot 读取会话结束评分所需快照（runtime finish 钩子用）。
	// 会话不存在时返回 (nil, nil)；TechScoreSet 用作重复回调的幂等守卫。
	GetSessionFinishSnapshot(ctx context.Context, sessionID int64) (*SessionFinishSnapshot, error)
	// ListSessionOperations 读取操作时间线并映射为评分操作。
	// session_operations 表尚未部署时返回空切片（操作代价维度按中性处理）。
	ListSessionOperations(ctx context.Context, sessionID int64) ([]model.OperationAction, error)
	// UpdateSessionTechScore 回写技术分、技术雷达与修复结论到 training_sessions。
	UpdateSessionTechScore(
		ctx context.Context,
		sessionID int64,
		techScore float64,
		techRadar model.RadarData,
		faultResolved bool,
	) error
}

// HintUsageReader 会话提示用量只读端口（技术分"信息检索"维输入）。
// HintQuotaProvider 天然满足；与配额/难度解耦，评分只消费已用次数。
type HintUsageReader interface {
	Usage(ctx context.Context, sessionID int64) (HintUsage, error)
}

// SessionFinishSnapshot 会话结束时的评分快照（技术分输入与会话归属信息）。
type SessionFinishSnapshot struct {
	SessionID        int64
	UserID           int64
	Status           string
	Difficulty       string
	ActualDuration   *int // 秒，可能为空；为空时用 StartedAt/EndedAt 推算
	StartedAt        *time.Time
	EndedAt          *time.Time
	TotalDefectCount int
	FixedDefectCount int
	TechScoreSet     bool // tech_score IS NOT NULL：已评分，重复回调跳过
	FaultResolved    bool // 已持久化的修复结论，后台关班评分保持 finish 的资格条件
}

// CollabEvaluator 协作评估端口（AIClient 天然满足）。
// 严格模式：实现方在 ai-service 不可用时返回错误，编排层不做本地规则兜底。
type CollabEvaluator interface {
	EvaluateCollab(ctx context.Context, req *aiv1.EvaluateCollabRequest) (*aiv1.EvaluateCollabResponse, error)
}

// ReviewCollabDetail 回写 reviews 用的协作分明细。
type ReviewCollabDetail struct {
	CollabOverall          float64
	QuestionQuality        float64
	InfoUtilization        float64
	Judgment               float64
	ErrorRecognitionRate   float64
	EvidenceValidationRate float64
	PitfallTotal           int
	PitfallRecognized      int
}

// SessionScoreResult 会话评分查询结果（API 返回用）。
type SessionScoreResult struct {
	SessionID              int64            `json:"session_id"`
	TechScore              float64          `json:"tech_score"`
	HasTechScore           bool             `json:"has_tech_score"`
	CollabScore            float64          `json:"collab_score"`
	HasCollabScore         bool             `json:"has_collab_score"`
	OverallScore           float64          `json:"overall_score"`
	CollabLevel            string           `json:"collab_level"`
	FaultResolved          bool             `json:"fault_resolved"`
	Difficulty             string           `json:"difficulty"`
	QuestionQuality        float64          `json:"question_quality"`
	InfoUtilization        float64          `json:"info_utilization"`
	Judgment               float64          `json:"judgment"`
	ErrorRecognitionRate   float64          `json:"error_recognition_rate"`
	EvidenceValidationRate float64          `json:"evidence_validation_rate"`
	PitfallCount           int              `json:"pitfall_count"`
	PitfallRecognized      int              `json:"pitfall_recognized"`
	CurrentCollabRadar     *model.RadarData `json:"current_collab_radar,omitempty"`
	AvgCollabRadar         *model.RadarData `json:"avg_collab_radar,omitempty"`
	AvgCollabScore         float64          `json:"avg_collab_score,omitempty"`
}

// FinishOutcome 云端 finish 回调显式声明的结束类型（最小闭环判定"正常完成"用）。
type FinishOutcome string

const (
	// FinishOutcomeUnspecified 回调未携带结束类型（当前云端契约为无 body）。
	FinishOutcomeUnspecified FinishOutcome = ""
	// FinishOutcomeCompleted 正常完成。
	FinishOutcomeCompleted FinishOutcome = "completed"
	// FinishOutcomeAborted 中止（用户主动/系统终止）。
	FinishOutcomeAborted FinishOutcome = "aborted"
	// FinishOutcomeTimeout 超时。
	FinishOutcomeTimeout FinishOutcome = "timeout"
)

// ScoreService 评分汇总服务。
type ScoreService struct {
	store     ScoreStore
	evaluator CollabEvaluator
	weights   Weights
	// hints 提示用量供给；未装配时 HintCount 视为 0（信息检索维满分）。
	hints HintUsageReader
	// expectedSecByDifficulty 各难度档参考时长（秒），用于技术分耗时维度；
	// 命中难度为 0 时该维度由 DurationScore 给中性分 60。
	expectedSecByDifficulty map[string]int
}

// defaultExpectedSecByDifficulty 难度参考时长代码默认值（秒）。
// 与 hint_quota_repo 一致，medium 的历史写法 normal 一并归一。
func defaultExpectedSecByDifficulty() map[string]int {
	return map[string]int{
		"easy":   300,
		"normal": 600,
		"medium": 600,
		"hard":   900,
	}
}

// NewScoreService 创建评分服务；weights 传 nil 用默认。
// evaluator 为 ai-service 协作评估客户端，不可为 nil。
func NewScoreService(store ScoreStore, evaluator CollabEvaluator, weights *Weights) *ScoreService {
	w := DefaultWeights()
	if weights != nil {
		w = *weights
	}
	return &ScoreService{
		store:                   store,
		evaluator:               evaluator,
		weights:                 w,
		expectedSecByDifficulty: defaultExpectedSecByDifficulty(),
	}
}

// SetExpectedSecByDifficulty 覆盖难度参考时长（装配层从配置注入；nil 重置为代码默认）。
func (s *ScoreService) SetExpectedSecByDifficulty(m map[string]int) {
	if m == nil {
		s.expectedSecByDifficulty = defaultExpectedSecByDifficulty()
		return
	}
	s.expectedSecByDifficulty = m
}

// SetHintUsageReader 装配提示用量供给（HintQuotaRepo 天然满足）。
// 未装配时技术分提示次数按 0 处理，保持最小闭环可用。
func (s *ScoreService) SetHintUsageReader(r HintUsageReader) {
	s.hints = r
}

// FinishSessionResult 会话结束评分结果。
type FinishSessionResult struct {
	Scored   bool    // 是否实际计分（未正常完成/已评过分时为 false）
	Reason   string  // 未计分原因（Scored=false 时有值，供日志观测）
	Result   *Result // 双维评分结果（Scored=true 时有值）
	TechOnly bool    // 技术分已落库但协作评估失败（尽力而为降级，仍视为已计分）
}

// FinishSessionScore runtime finish 钩子触发的会话结束评分（最小闭环）。
//
// 语义：
//   - 仅"正常完成"计分；中止/超时/异常不计分（不写技术分与协作分）。
//   - 幂等：tech_score 已存在的会话直接跳过（云端重复回调安全）。
//   - 技术分完全本地计算，先落库；协作分在技术分落库后尽力而为，
//     ai-service 不可用不影响技术分结论（返回 TechOnly=true）。
//
// 正常完成判定优先级：回调显式 outcome > 会话 status > 缺陷修复计数启发式。
// 当前云端 finish 契约无 body，outcome 通常为 Unspecified，故主要依赖
// status（completed/resolved）与 fixed>=total 启发式；待云端补结束类型后即精确。
func (s *ScoreService) FinishSessionScore(
	ctx context.Context,
	sessionID int64,
	outcome FinishOutcome,
) (*FinishSessionResult, error) {
	return s.runSessionScore(ctx, sessionID, outcome, false)
}

func (s *ScoreService) finishSessionScore(ctx context.Context, sessionID int64, outcome FinishOutcome, requireResolved bool) (*FinishSessionResult, error) {
	snap, err := s.store.GetSessionFinishSnapshot(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取会话快照: %w", err)
	}
	if snap == nil {
		return &FinishSessionResult{Scored: false, Reason: "会话不存在"}, nil
	}
	if requireResolved && (snap.Status != "completed" || !snap.FaultResolved) {
		return &FinishSessionResult{Scored: false, Reason: "未完成修复，不计分"}, nil
	}
	if snap.TechScoreSet {
		return &FinishSessionResult{Scored: false, Reason: "会话已评分，跳过（幂等）"}, nil
	}

	resolved := resolveFault(snap)
	if !isNormalFinish(outcome, snap.Status, resolved) {
		return &FinishSessionResult{Scored: false, Reason: "非正常完成（中止/超时/异常），不计分"}, nil
	}
	if ready, ok := s.store.(interface {
		WaitForOperations(context.Context, int64) error
	}); ok {
		if err := ready.WaitForOperations(ctx, sessionID); err != nil {
			return nil, fmt.Errorf("等待操作投影: %w", err)
		}
	}

	// 技术分输入组装：
	//   耗时：actual_duration 优先，缺失用 ended_at-started_at 推算，再缺为 0。
	//   参考时长：按难度取配置，未命中为 0（耗时维度由纯函数给中性 60）。
	//   操作代价：操作时间线映射计算；表未部署/无记录时为空，惩罚 0（代价维满分=中性）。
	//   提示次数：从 HintQuotaRepo.Usage 实时重建；未装配按 0（信息检索维满分）。
	actions, err := s.store.ListSessionOperations(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取操作时间线: %w", err)
	}
	costResult := CalcCostPenalty(actions)
	hintCount := 0
	if s.hints != nil {
		usage, uerr := s.hints.Usage(ctx, sessionID)
		if uerr != nil {
			return nil, fmt.Errorf("读取提示用量: %w", uerr)
		}
		hintCount = usage.Used
	}
	techIn := TechInput{
		FaultResolved: resolved,
		DurationSec:   resolveDurationSec(snap),
		ExpectedSec:   s.expectedSecByDifficulty[snap.Difficulty],
		CostPenalty:   costResult.Penalty,
		HintCount:     hintCount,
	}
	tech := TechScore(techIn, s.weights)
	techRadar := BuildTechRadar(techIn, s.weights)

	// 技术分与技术雷达先落库（本地、确定性，是最小闭环的核心产物）。
	if err := s.store.UpdateSessionTechScore(ctx, sessionID, tech, techRadar, resolved); err != nil {
		return nil, fmt.Errorf("回写技术分: %w", err)
	}

	// 协作分尽力而为：ai-service 不可用/采集失败不回滚技术分。
	result, err := s.runCollabPipeline(ctx, snap.SessionID, snap.UserID, techIn)
	if err != nil {
		return &FinishSessionResult{
			Scored:   true,
			TechOnly: true,
			Result:   &Result{Tech: tech},
			Reason:   "技术分已落库，协作评估失败: " + err.Error(),
		}, nil
	}
	return &FinishSessionResult{Scored: true, Result: result}, nil
}

// isNormalFinish 判定会话是否"正常完成"（仅正常完成计分）。
func isNormalFinish(outcome FinishOutcome, status string, resolved bool) bool {
	switch outcome {
	case FinishOutcomeCompleted:
		return true
	case FinishOutcomeAborted, FinishOutcomeTimeout:
		return false
	}
	// 回调未声明结束类型：看会话状态机。
	switch status {
	case "completed", "resolved":
		return true
	case "aborted", "failed":
		return false
	}
	// 状态仍停在 running（生命周期落库尚未接线）时，以缺陷修复计数启发式兜底。
	return resolved
}

// resolveFault 判定故障是否修复。
// total>0 时要求 fixed>=total；total=0（缺陷计数未登记）时无法据此判定，保守为 false，
// 避免把"未完成登记"误判成"已修复"。
func resolveFault(snap *SessionFinishSnapshot) bool {
	if snap.TotalDefectCount <= 0 {
		return false
	}
	return snap.FixedDefectCount >= snap.TotalDefectCount
}

// resolveDurationSec 取实际耗时（秒）：actual_duration 优先，其次 ended-started。
func resolveDurationSec(snap *SessionFinishSnapshot) int {
	if snap.ActualDuration != nil && *snap.ActualDuration > 0 {
		return *snap.ActualDuration
	}
	if snap.StartedAt != nil && snap.EndedAt != nil {
		d := int(snap.EndedAt.Sub(*snap.StartedAt).Seconds())
		if d > 0 {
			return d
		}
	}
	return 0
}

// ComputeAndWriteCollabScore 计算并回写协作分（T3-24 核心，严格模式）。
//
// 流程：收集协作事件 → 远程取 ai-service 三维子分 → 加权合并 → 构造雷达 → 分类回写。
// 严格模式：事件采集或 ai-service 评估失败时直接返回错误，不写任何协作表，
// 也不在本服务保留本地规则兜底（避免与 ai-service 双实现产生分叉）。
// 纯人工模式（响应 available=false）属正常业务结果：综合分即技术分，
// reviews 写入零值明细，training_sessions 与用户协作档案不更新。
//
// 返回计算结果，供调用方日志/调试。
func (s *ScoreService) ComputeAndWriteCollabScore(
	ctx context.Context,
	sessionID, userID int64,
	techIn TechInput,
) (*Result, error) {
	return s.runCollabPipeline(ctx, sessionID, userID, techIn)
}

// runCollabPipeline 协作评估与回写管线（严格模式），供严格入口与结束评分复用。
// 事件采集或 ai-service 评估失败即返回错误；回写各表尽力而为不阻断。
func (s *ScoreService) runCollabPipeline(
	ctx context.Context,
	sessionID, userID int64,
	techIn TechInput,
) (*Result, error) {
	// 1) 收集协作行为事件
	events, err := s.store.CollectCollabInput(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("收集协作事件: %w", err)
	}

	// 2) 请求 ai-service 做确定性协作评估（失败即终止，不本地兜底）
	resp, err := s.evaluator.EvaluateCollab(ctx, buildEvaluateRequest(sessionID, events))
	if err != nil {
		return nil, fmt.Errorf("协作评估远程调用: %w", err)
	}

	// 3) 用 ai-service 子分做双维加权合并
	sub := CollabSubScores{
		QuestionQuality: resp.QuestionQualityScore,
		InfoUtilization: resp.InfoUtilizationScore,
		Judgment:        resp.JudgmentScore,
		Available:       resp.Available,
	}
	result := Evaluate(techIn, sub, s.weights)

	// 4) 构造协作维雷达（两率与计数取自 ai-service；独立性依赖本地提问数+提示数）
	independence := calcIndependenceScore(len(events.Questions), techIn)
	radarInput := CollabRadarInput{
		QuestionQuality:   sub.QuestionQuality,
		InfoUtilization:   sub.InfoUtilization,
		Judgment:          sub.Judgment,
		PitfallAwareness:  resp.ErrorRecognitionRate,
		VerificationHabit: resp.EvidenceValidationRate,
		Independence:      independence,
	}
	collabRadar := BuildCollabRadar(radarInput)

	// 5) 回写 training_sessions.collab_score（仅存在有效协作分时）
	if result.HasCollab {
		_ = s.store.UpdateSessionCollabScore(ctx, sessionID, result.Collab)
	}

	// 6) 回写 reviews 协作明细（纯人工模式同样落零值明细，语义为"无协作数据"）
	detail := ReviewCollabDetail{
		CollabOverall:          result.Collab,
		QuestionQuality:        sub.QuestionQuality,
		InfoUtilization:        sub.InfoUtilization,
		Judgment:               sub.Judgment,
		ErrorRecognitionRate:   resp.ErrorRecognitionRate,
		EvidenceValidationRate: resp.EvidenceValidationRate,
		PitfallTotal:           int(resp.PitfallTotal),
		PitfallRecognized:      int(resp.PitfallRecognized),
	}
	_ = s.store.UpdateReviewCollabScores(ctx, sessionID, detail)

	// 7) 经 user-service 更新用户协作档案（滚动雷达 / 均分 / 等级，仅有效协作分）
	if result.HasCollab {
		_ = s.store.UpdateUserProfileCollab(
			ctx, userID, collabRadar, result.Collab, result.Level,
		)
	}

	return &result, nil
}

// buildEvaluateRequest 把本地采集事件映射为 ai-service 契约请求。
// 未知结局/操作类型归一到 PENDING / INDEPENDENT（与 proto UNSPECIFIED 的服务端兜底一致）。
func buildEvaluateRequest(sessionID int64, e CollabEvents) *aiv1.EvaluateCollabRequest {
	req := &aiv1.EvaluateCollabRequest{
		SessionId: sessionID,
		Questions: make([]*aiv1.CollabQuestion, 0, len(e.Questions)),
		Pitfalls:  make([]*aiv1.CollabPitfall, 0, len(e.Pitfalls)),
		Actions:   make([]*aiv1.CollabAction, 0, len(e.Actions)),
	}
	for _, q := range e.Questions {
		req.Questions = append(req.Questions, &aiv1.CollabQuestion{
			Text:                q.Text,
			Stage:               q.Stage,
			ReferencesKnownInfo: q.ReferencesKnownInfo,
			FollowUp:            q.FollowUp,
		})
	}
	for _, p := range e.Pitfalls {
		req.Pitfalls = append(req.Pitfalls, &aiv1.CollabPitfall{
			Outcome: toProtoPitfallOutcome(p.Outcome),
		})
	}
	for _, a := range e.Actions {
		req.Actions = append(req.Actions, &aiv1.CollabAction{
			Kind:                 toProtoActionKind(a.Kind),
			VerifiedBeforeFollow: a.VerifiedBeforeFollow,
		})
	}
	return req
}

func toProtoPitfallOutcome(o PitfallOutcome) aiv1.PitfallOutcome {
	switch o {
	case PitfallOutcomeRecognized:
		return aiv1.PitfallOutcome_PITFALL_OUTCOME_RECOGNIZED
	case PitfallOutcomeFell:
		return aiv1.PitfallOutcome_PITFALL_OUTCOME_FELL
	default:
		return aiv1.PitfallOutcome_PITFALL_OUTCOME_PENDING
	}
}

func toProtoActionKind(k CollabActionKind) aiv1.ActionKind {
	if k == CollabActionFollowAI {
		return aiv1.ActionKind_ACTION_KIND_FOLLOW_AI
	}
	return aiv1.ActionKind_ACTION_KIND_INDEPENDENT
}

// GetSessionScore 查询会话评分详情（含雷达数据）。
func (s *ScoreService) GetSessionScore(
	ctx context.Context,
	sessionID, userID int64,
) (*SessionScoreResult, error) {
	detail, err := s.store.GetSessionScoreDetail(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, nil
	}

	// 构造当前协作雷达（从明细数据）
	if detail.HasCollabScore {
		radarInput := CollabRadarInput{
			QuestionQuality:   detail.QuestionQuality,
			InfoUtilization:   detail.InfoUtilization,
			Judgment:          detail.Judgment,
			PitfallAwareness:  detail.ErrorRecognitionRate,
			VerificationHabit: detail.EvidenceValidationRate,
			// 独立性查询时不重算，用协作分做近似（MVP 简化）
			Independence: detail.CollabScore * 0.8,
		}
		radar := BuildCollabRadar(radarInput)
		detail.CurrentCollabRadar = &radar
	}

	// 计算综合分（技术分缺失时只用协作分，反之亦然）
	overall := 0.0
	if detail.HasTechScore && detail.HasCollabScore {
		overall = detail.TechScore*s.weights.OverallTechWeight +
			detail.CollabScore*s.weights.OverallCollabWeight
	} else if detail.HasTechScore {
		overall = detail.TechScore
	} else if detail.HasCollabScore {
		overall = detail.CollabScore
	}

	return &SessionScoreResult{
		SessionID:              detail.SessionID,
		TechScore:              detail.TechScore,
		HasTechScore:           detail.HasTechScore,
		CollabScore:            detail.CollabScore,
		HasCollabScore:         detail.HasCollabScore,
		OverallScore:           overall,
		CollabLevel:            detail.CollabLevel,
		FaultResolved:          detail.FaultResolved,
		Difficulty:             detail.Difficulty,
		QuestionQuality:        detail.QuestionQuality,
		InfoUtilization:        detail.InfoUtilization,
		Judgment:               detail.Judgment,
		ErrorRecognitionRate:   detail.ErrorRecognitionRate,
		EvidenceValidationRate: detail.EvidenceValidationRate,
		PitfallCount:           detail.PitfallCount,
		PitfallRecognized:      detail.PitfallRecognized,
		CurrentCollabRadar:     detail.CurrentCollabRadar,
		AvgCollabRadar:         detail.AvgCollabRadar,
		AvgCollabScore:         detail.AvgCollabScore,
	}, nil
}

// calcIndependenceScore 计算独立性得分（0-100）。
//
// 维度：
//   - 提问少 = 更独立（提问越多分数越低）
//   - 提示少 = 更独立（与提示次数反向）
//
// MVP 简化版：提问数 0-5 档计分 + 提示数反向。
func calcIndependenceScore(qCount int, techIn TechInput) float64 {
	// 提问数得分：0 题 = 100，1-2 题 = 80，3-5 题 = 60，6-10 题 = 40，10+ = 20
	qScore := 100.0
	switch {
	case qCount == 0:
		qScore = 100
	case qCount <= 2:
		qScore = 80
	case qCount <= 5:
		qScore = 60
	case qCount <= 10:
		qScore = 40
	default:
		qScore = 20
	}

	// 提示数得分：0 次 = 100，每用一次扣 10，最低 20
	hintScore := 100.0 - float64(techIn.HintCount)*10.0
	if hintScore < 20 {
		hintScore = 20
	}

	// 加权：提问独立性 60% + 提示独立性 40%
	return qScore*0.6 + hintScore*0.4
}
