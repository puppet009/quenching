// 训练会话编排服务（个人中心 / 人机对照真接口）。
//
// 职责：
//   - 创建会话：交叉分组前两次走 CrossGroupPlanner（互补模式 + 校准故障），
//     第三次及以后回退普通创建
//   - 开始 / 结束：调仓储条件更新落实状态机
//   - 结束后由持久化会话状态驱动后台评分与复盘，不在 finish 响应中等待 AI
//   - 历史 / 概览 / 人机对照的聚合组装（对外驼峰契约）
//
// 注意：本包不得导入 internal/repository（repository 为共享类型反向依赖本包，
// 会构成 import cycle）。仓储交互全部使用本文件定义的端口类型。
package service

import (
	"context"
	"encoding/json"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	"math/rand"
	"strings"
	"time"

	"github.com/quenching/go-common/analytics"
	"github.com/quenching/training-service/internal/model"
)

// ——— 仓储交互类型（与 repository DTO 字段对齐，避免反向导入）———

// CreateSessionInput 创建会话入参。Difficulty 已归一为 DB 口径（easy/medium/hard）。
type CreateSessionInput struct {
	UserID       int64
	ProjectID    int64
	TemplateSlug string // 文件模板 slug；非空时按 templates/<slug> 装配，ProjectID 可传 0
	Difficulty   string
	AIMode       string // none / assistant
	ControlGroup string // A / B，未分组为空
	DefectID     int64  // 第二场挂接的校准故障；0 表示不挂
	Title        string
}

// SessionHistoryRow 历史列表行（字段对齐前端 TrainingHistoryItem）。
type SessionHistoryRow struct {
	ID                 int64      `json:"id"`
	Status             string     `json:"status"`
	BrainPhase         string     `json:"brainPhase,omitempty"`
	CleanupStatus      string     `json:"cleanupStatus,omitempty"`
	ReviewLookupFailed bool       `json:"reviewLookupFailed,omitempty"`
	ProjectName        string     `json:"projectName"`
	DefectName         string     `json:"defectName"`
	Difficulty         string     `json:"difficulty"`
	AIMode             string     `json:"aiMode"`
	TechScore          *float64   `json:"techScore"`
	CollabScore        *float64   `json:"collabScore"`
	Success            *bool      `json:"success"`
	DurationSec        int64      `json:"durationSec"`
	StartTime          *time.Time `json:"startTime"`
	HasReview          bool       `json:"hasReview"`
	ReviewID           *int64     `json:"reviewId"`
	CreatedAt          time.Time  `json:"createdAt"`
	// FirstDefectID 仓储内部承载：本地 SQL 取首故障 ID，再跨域回填名称，不对外序列化。
	FirstDefectID int64 `json:"-"`
	// ProjectID 仓储内部承载：本地 SQL 取项目 ID，再跨域回填名称，不对外序列化。
	ProjectID int64 `json:"-"`
}

// SessionListFilter 历史列表过滤与分页。
type SessionListFilter struct {
	UserID     int64
	Status     string // empty or finished (all terminal outcomes)
	Difficulty string
	AIMode     string
	Success    *bool
	Keyword    string
	Page       int
	PageSize   int
}

// SessionDetail 会话详情（训练页 header 数据源）。复用历史行的 JOIN 口径，
// 再补充状态、项目ID、控制组与开始时间。
type SessionDetail struct {
	ID           int64      `json:"id"`
	Status       string     `json:"status"`
	AIMode       string     `json:"aiMode"`
	ControlGroup string     `json:"controlGroup"`
	Difficulty   string     `json:"difficulty"`
	ProjectID    int64      `json:"projectId"`
	ProjectName  string     `json:"projectName"`
	DefectName   string     `json:"defectName"`
	StartTime    *time.Time `json:"startTime"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// MetricsAggregate 概览计数聚合。
type MetricsAggregate struct {
	TotalSessions int64
	Finished      int64
	Success       int64
	TotalDuration int64
}

// ScoreStats 已评分会话的均分。
type ScoreStats struct {
	AvgTechScore   *float64
	AvgCollabScore *float64
}

// SessionReviewRow 单个会话的技术雷达与 reviews 协作子分。
type SessionReviewRow struct {
	ID                     int64
	TechRadarJSON          *string
	QuestionQuality        *float64
	InfoUtilization        *float64
	Judgment               *float64
	ErrorRecognitionRate   *float64
	EvidenceValidationRate *float64
	Independence           *float64
}

// LoadTechRadar 解析 tech_radar JSON；为空或解析失败返回零值 RadarData。
func (row *SessionReviewRow) LoadTechRadar() model.RadarData {
	var radar model.RadarData
	if row != nil && row.TechRadarJSON != nil && *row.TechRadarJSON != "" {
		_ = json.Unmarshal([]byte(*row.TechRadarJSON), &radar)
	}
	return radar
}

// ComparisonSessionRow 对照单侧的指标聚合行。
type ComparisonSessionRow struct {
	ID                int64
	AIMode            string
	Difficulty        string
	Success           *bool
	DurationSec       int64
	TechScore         *float64
	CollabScore       *float64
	PitfallTotal      int64
	PitfallRecognized int64
	DefectName        string
}

// OperationInput 单条操作上报。
type OperationInput = oe.LegacyInput

// SessionStore 会话仓储端口（由 repository.SessionRepo 实现）。
type SessionStore interface {
	CreateSession(ctx context.Context, in CreateSessionInput) (int64, error)
	StartSession(ctx context.Context, sessionID, userID int64) (bool, error)
	FinishSession(ctx context.Context, in FinishSessionInput) (bool, error)
	// AbandonSession 系统判定弃班：活跃会话置为 aborted；已是终态返回 false。
	AbandonSession(ctx context.Context, sessionID int64) (bool, error)
	GetSessionStatus(ctx context.Context, sessionID, userID int64) (string, bool, error)
	GetSessionDetail(ctx context.Context, sessionID, userID int64) (*SessionDetail, error)
	AddOperations(ctx context.Context, sessionID, userID int64, ops []OperationInput) (int, error)
	ListSessions(ctx context.Context, f SessionListFilter) ([]SessionHistoryRow, int64, error)
	GetMetrics(ctx context.Context, userID int64) (MetricsAggregate, error)
	GetScoreStats(ctx context.Context, userID int64) (ScoreStats, error)
	LatestScoredRadar(ctx context.Context, userID int64) (model.RadarData, model.RadarData, error)
	GetComparisonPair(ctx context.Context, userID int64) (*ComparisonSessionRow, *ComparisonSessionRow, error)
	GetSessionReviewRow(ctx context.Context, sessionID int64) (*SessionReviewRow, error)
	CountSessionOperations(ctx context.Context, sessionID int64) (int64, int64, int64, error)
}

// FinishSessionInput 结束会话入参。
type FinishSessionInput struct {
	SessionID int64
	UserID    int64
	Status    string
	Resolved  bool
	Duration  *time.Duration // 显式实际耗时；nil 时由仓储按 started_at 计算
}

// SessionScorer 结束后评分端口（由 ScoreService 实现）。
type SessionScorer interface {
	FinishSessionScore(ctx context.Context, sessionID int64, outcome FinishOutcome) (*FinishSessionResult, error)
}

// SessionService 训练会话编排服务。
type SessionService struct {
	brain interface {
		Freeze(context.Context, int64, string) error
	}
	readiness       func(context.Context, int64, int64) (bool, error)
	store           SessionStore
	planner         *CrossGroupPlanner
	scorer          SessionScorer
	summaryStreamer ComparisonSummaryStreamer
	// lease 向沙箱侧回写活跃度/结束/回收；未接线时为 nil（静默跳过）。
	lease *activityLease
	// tracker 服务端埋点；未接线时为 nil（不上报）。
	tracker *analytics.Client
}

// NewSessionService 创建会话编排服务。
func NewSessionService(store SessionStore, planner *CrossGroupPlanner, scorer SessionScorer) *SessionService {
	return &SessionService{store: store, planner: planner, scorer: scorer}
}

// SetSandboxLifecycle 注入沙箱生命周期回写端口（装配层 setter，保持构造函数稳定）。
// 传入 nil 表示 sandbox_service.base_url 未配置，实训不受影响。
func (s *SessionService) SetSandboxLifecycle(client SandboxLifecycle) {
	s.lease = newActivityLease(client, 0)
}

// SetComparisonSummaryStreamer 注入人机对照总结流式端口（装配层 setter 注入，
// 保持构造函数签名稳定）。未注入时 StreamComparisonSummary 直接失败，
// 前端据此降级使用规则文案。
func (s *SessionService) SetComparisonSummaryStreamer(streamer ComparisonSummaryStreamer) {
	s.summaryStreamer = streamer
}

// SetAnalytics 注入服务端埋点客户端（装配层 setter，保持构造函数签名稳定）。
// 传入 nil 表示 review_service.base_url 未配置，不上报。
func (s *SessionService) SetAnalytics(c *analytics.Client) {
	s.tracker = c
}

// ——— 对外 DTO（驼峰契约）———

// CreateSessionRequest 创建会话请求（前端第六片对接）。
type CreateSessionRequest struct {
	ProjectID    int64  `json:"project_id"`
	TemplateSlug string `json:"template_slug"` // 文件模板 slug；与 project_id 至少传一个
	Difficulty   string `json:"difficulty"`    // easy/normal/hard（normal 落库归一为 medium）
}

// CreateSessionResponse 创建会话响应。
type CreateSessionResponse struct {
	ID           int64  `json:"id"`
	Status       string `json:"status"`
	AIMode       string `json:"aiMode"`
	ControlGroup string `json:"controlGroup"`
}

// StartSessionResponse 开始会话响应。
type StartSessionResponse struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// FinishSessionRequest 结束会话请求。
type FinishSessionRequest struct {
	Outcome string `json:"outcome"` // completed / aborted / failed
}

// FinishSessionResponse 确认结束状态已持久化；评分和复盘在后台继续。
type FinishSessionResponse struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Resolved bool   `json:"resolved"`
	Scored   bool   `json:"scored"`
}

// OperationsRequest 操作上报请求。
type OperationsRequest struct {
	Operations []OperationInput `json:"operations"`
}

// OperationsResponse 操作上报响应。
type OperationsResponse struct {
	Accepted int `json:"accepted"`
}

// HistoryPage 历史分页。
type HistoryPage struct {
	List     []SessionHistoryRow `json:"list"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

// OverviewStats 概览统计。
type OverviewStats struct {
	TotalSessions  int64    `json:"totalSessions"`
	Finished       int64    `json:"finished"`
	SuccessRate    int64    `json:"successRate"` // 百分比 0-100
	TotalMinutes   int64    `json:"totalMinutes"`
	AvgTechScore   *float64 `json:"avgTechScore"`
	AvgCollabScore *float64 `json:"avgCollabScore"`
}

// OverviewResponse 概览响应（统计 + 双维雷达）。
type OverviewResponse struct {
	Stats OverviewStats          `json:"stats"`
	Radar *DualDimensionRadarDTO `json:"radar"`
}

// DualDimensionRadarDTO 对外驼峰双维雷达（与前端 DualDimensionRadar 对齐）。
type DualDimensionRadarDTO struct {
	CurrentTech   model.RadarData  `json:"currentTech"`
	CurrentCollab model.RadarData  `json:"currentCollab"`
	AvgTech       *model.RadarData `json:"avgTech,omitempty"`
	AvgCollab     *model.RadarData `json:"avgCollab,omitempty"`
	TechOverall   float64          `json:"techOverall"`
	CollabOverall float64          `json:"collabOverall"`
	HasCollab     bool             `json:"hasCollab"`
	Overall       float64          `json:"overall"`
	Level         string           `json:"level"`
}

// TrainingMetricsDTO 人机对照单侧指标（驼峰，对齐前端 TrainingMetrics）。
type TrainingMetricsDTO struct {
	SessionID         int64    `json:"sessionId"`
	DefectName        string   `json:"defectName"`
	Success           *bool    `json:"success"`
	TotalTimeSec      int64    `json:"totalTimeSec"`
	TechScore         *float64 `json:"techScore"`
	CollabScore       *float64 `json:"collabScore"`
	DangerousOps      int64    `json:"dangerousOps"`
	VerifyActions     int64    `json:"verifyActions"`
	AIQuestions       int64    `json:"aiQuestions"`
	PitfallRecognized int64    `json:"pitfallRecognized"`
	PitfallTotal      int64    `json:"pitfallTotal"`
}

// ComparisonResponse 人机对照响应。提升百分比由前端计算；AI 总结由
// POST /training/comparison/summary（ai-service SSE）单独流式产出，本接口只给真数据。
type ComparisonResponse struct {
	Manual      *TrainingMetricsDTO `json:"manual"`
	AICollab    *TrainingMetricsDTO `json:"aiCollab"`
	TechRadar   RadarPair           `json:"techRadar"`
	CollabRadar RadarPair           `json:"collabRadar"`
}

// RadarPair 两侧雷达。
type RadarPair struct {
	Manual   model.RadarData `json:"manual"`
	AICollab model.RadarData `json:"aiCollab"`
}

// ——— 创建 ———

// Create 创建会话。交叉分组前两次：planner 决定尝试次、模式与（第二次的）校准故障；
// 第三次及以后 planner 返回 ErrBeyondCrossGroup，回退普通创建（默认 AI 协作模式）。
func (s *SessionService) Create(ctx context.Context, userID int64, req CreateSessionRequest) (*CreateSessionResponse, error) {
	templateSlug := strings.TrimSpace(req.TemplateSlug)
	if req.ProjectID <= 0 && templateSlug == "" {
		return nil, ErrSessionProjectRequired
	}
	difficulty := NormalizeDifficulty(req.Difficulty)

	// 文件模板实训不挂载 fault-engine 故障，不满足交叉分组第二场
	// "同类同难度校准故障"的前提；直接走普通创建（默认 AI 协作模式），
	// 避免 planner 因首场无故障报错。
	if templateSlug != "" {
		return s.createOrdinary(ctx, userID, req.ProjectID, templateSlug, difficulty)
	}

	plan, planErr := s.planner.PrepareSessionPlan(ctx, userID)
	if planErr != nil && !errors.Is(planErr, ErrBeyondCrossGroup) {
		return nil, planErr
	}

	if plan != nil {
		return s.createPlanned(ctx, userID, req.ProjectID, templateSlug, difficulty, plan)
	}
	return s.createOrdinary(ctx, userID, req.ProjectID, templateSlug, difficulty)
}

// createPlanned 落盘交叉分组前两次会话。
func (s *SessionService) createPlanned(
	ctx context.Context, userID, projectID int64, templateSlug, difficulty string, plan *SessionPlan,
) (*CreateSessionResponse, error) {
	in := CreateSessionInput{
		UserID:       userID,
		ProjectID:    projectID,
		TemplateSlug: templateSlug,
		Difficulty:   difficulty,
		AIMode:       string(plan.AIMode),
		ControlGroup: string(plan.Group),
	}

	// 第二次：挂接 planner 选定的同类同难度校准故障，落盘后绑定配对。
	if plan.Attempt == AttemptSecond {
		if plan.PairedDefect == nil {
			return nil, errors.New("交叉分组第二场缺少校准故障")
		}
		in.DefectID = plan.PairedDefect.ID
	}

	id, err := s.store.CreateSession(ctx, in)
	if err != nil {
		return nil, err
	}

	if plan.Attempt == AttemptSecond {
		if _, err := s.planner.BindPair(ctx, id); err != nil {
			return nil, err
		}
	}

	return &CreateSessionResponse{
		ID:           id,
		Status:       string(model.StatusPending),
		AIMode:       string(plan.AIMode),
		ControlGroup: string(plan.Group),
	}, nil
}

// createOrdinary 普通创建（第三次及以后）：默认 AI 协作模式、无固定故障，
// 控制组沿用 planner 已分配的（EnsureGroup 幂等），分配不到也不阻断。
func (s *SessionService) createOrdinary(
	ctx context.Context, userID, projectID int64, templateSlug, difficulty string,
) (*CreateSessionResponse, error) {
	group := ""
	if g, err := s.planner.EnsureGroup(ctx, userID); err == nil {
		group = string(g)
	}

	in := CreateSessionInput{
		UserID:       userID,
		ProjectID:    projectID,
		TemplateSlug: templateSlug,
		Difficulty:   difficulty,
		AIMode:       string(model.AIModeAssist),
		ControlGroup: group,
	}
	id, err := s.store.CreateSession(ctx, in)
	if err != nil {
		return nil, err
	}
	return &CreateSessionResponse{
		ID:           id,
		Status:       string(model.StatusPending),
		AIMode:       string(model.AIModeAssist),
		ControlGroup: group,
	}, nil
}

// ——— 开始 ———

// SetSandboxReadiness keeps queued provisioning out of training time and brain dispatch.
func (s *SessionService) SetSandboxReadiness(check func(context.Context, int64, int64) (bool, error)) {
	s.readiness = check
}

// Start 把会话推进到 running。
func (s *SessionService) Start(ctx context.Context, sessionID, userID int64) (*StartSessionResponse, error) {
	if s.readiness != nil {
		_, found, err := s.store.GetSessionStatus(ctx, sessionID, userID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, ErrSessionNotFound
		}
		ready, err := s.readiness(ctx, sessionID, userID)
		if err != nil {
			return nil, err
		}
		if !ready {
			return nil, ErrSessionNotStartable
		}
	}
	ok, err := s.store.StartSession(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		// 区分不存在 / 已在进行 / 已结束，给出可读原因。
		status, found, ferr := s.store.GetSessionStatus(ctx, sessionID, userID)
		if ferr != nil {
			return nil, ferr
		}
		if !found {
			return nil, ErrSessionNotFound
		}
		if model.SessionStatus(status) == model.StatusRunning {
			return &StartSessionResponse{ID: sessionID, Status: status}, nil
		}
		return nil, ErrSessionNotStartable
	}
	return &StartSessionResponse{ID: sessionID, Status: string(model.StatusRunning)}, nil
}

// ——— 结束 ———

// Finish 确认结束状态已持久化。后台扫描会继续评分、回收和复盘，
// 请求不等待这些工作，也不承担它们的重试生命周期。
func (s *SessionService) Finish(ctx context.Context, sessionID, userID int64, req FinishSessionRequest) (*FinishSessionResponse, error) {
	status := req.Outcome
	if status == "" {
		status = string(model.StatusCompleted)
	}
	outcome := model.SessionStatus(status)
	if outcome != model.StatusCompleted &&
		outcome != model.StatusAborted &&
		outcome != model.StatusFailed {
		return nil, ErrInvalidOutcome
	}

	before, found, err := s.store.GetSessionStatus(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrSessionNotFound
	}

	resolved := outcome == model.StatusCompleted
	if reader, ok := s.brain.(interface {
		VerifiedRepair(context.Context, int64) (bool, bool, error)
	}); ok {
		repaired, enabled, e := reader.VerifiedRepair(ctx, sessionID)
		if e != nil {
			return nil, e
		}
		if enabled {
			resolved = repaired
		}
	}

	// 已在终态：幂等返回，不重复评分。
	beforeStatus := model.SessionStatus(before)
	if beforeStatus == model.StatusCompleted ||
		beforeStatus == model.StatusAborted ||
		beforeStatus == model.StatusFailed {
		return &FinishSessionResponse{
			ID:       sessionID,
			Status:   before,
			Resolved: resolved,
			Scored:   false,
		}, nil
	}

	if s.brain != nil {
		if err := s.brain.Freeze(ctx, sessionID, status); err != nil {
			return nil, err
		}
	}

	ok, err := s.store.FinishSession(ctx, FinishSessionInput{
		SessionID: sessionID,
		UserID:    userID,
		Status:    status,
		Resolved:  resolved,
		// Duration 传 nil：由仓储按 started_at 自动计算 actual_duration。
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionNotFinishable
	}

	// 实训结束：回写沙箱结束时刻，进入结束宽限期后由回收扫描释放（复盘可查看完再回收）。
	s.lease.force(sessionID, func(cctx context.Context) error {
		return s.leaseFinishReason(cctx, sessionID, outcome)
	}, "结束回写")
	s.lease.forget(sessionID)

	// 完成训练上报 first_complete。生产端无需判断是否首训：
	// 唯一事件重复写入时由 review-service 自动补记 retrain。
	if outcome == model.StatusCompleted {
		s.tracker.Track(ctx, analytics.Event{
			EventType: "first_complete",
			UserID:    userID,
			SessionID: sessionID,
			Platform:  "server",
		})
	}

	return &FinishSessionResponse{
		ID:       sessionID,
		Status:   status,
		Resolved: resolved,
		Scored:   false,
	}, nil
}

// 沙箱结束原因（与 sandbox-service service.Reason* 常量取值一致）。
const (
	sandboxReasonCompleted = "completed"
	sandboxReasonManual    = "manual"
	sandboxReasonError     = "error"
	sandboxReasonAbandoned = "abandoned"
)

// leaseFinishReason 把会话终态映射为沙箱侧的结束原因。
// 只在 activityLease.force 的回写闭包内调用：未接线时 force 自己短路，这里无需再判空。
func (s *SessionService) leaseFinishReason(ctx context.Context, sessionID int64, outcome model.SessionStatus) error {
	reason := sandboxReasonManual
	switch outcome {
	case model.StatusCompleted:
		reason = sandboxReasonCompleted
	case model.StatusFailed:
		reason = sandboxReasonError
	}
	return s.lease.client.MarkCompleted(ctx, sessionID, reason)
}

// Abandon 弃班收口：长时间无活动由卡顿扫描判定后调用。
// 会话置为 aborted（复盘按"未完成"出，不计分），沙箱当场回收释放稀缺槽位。
// 已终态的会话幂等跳过（用户可能刚好在判定瞬间提交了修复）。
func (s *SessionService) Abandon(ctx context.Context, sessionID int64) (bool, error) {
	if sessionID <= 0 {
		return false, ErrSessionNotFound
	}
	ok, err := s.store.AbandonSession(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil // 非活跃态：已被用户结束或从未开始，不重复弃班
	}
	s.lease.forget(sessionID)
	s.lease.force(sessionID, func(cctx context.Context) error {
		return s.lease.client.ReleaseNow(cctx, sessionID, sandboxReasonAbandoned)
	}, "弃班回收")
	return true, nil
}

// ——— 操作上报 ———

// AddOperations 批量上报操作。
func (s *SessionService) AddOperations(
	ctx context.Context, sessionID, userID int64, req OperationsRequest,
) (*OperationsResponse, error) {
	if len(req.Operations) == 0 {
		return &OperationsResponse{Accepted: 0}, nil
	}
	n, err := s.store.AddOperations(ctx, sessionID, userID, req.Operations)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	// 有操作落库即证明学员还在干活：向沙箱侧续租（限流、异步、失败只记日志）。
	s.lease.touch(sessionID)
	return &OperationsResponse{Accepted: n}, nil
}

// ——— 历史 ———

// Detail 返回单个会话详情；不存在或不属于当前用户时返回 ErrSessionNotFound。
func (s *SessionService) Detail(ctx context.Context, sessionID, userID int64) (*SessionDetail, error) {
	if sessionID <= 0 {
		return nil, ErrSessionNotFound
	}
	detail, err := s.store.GetSessionDetail(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, ErrSessionNotFound
	}
	// 盲盒口径：实训进行中的会话不回传故障名，避免开场预告（复盘/历史等终态才公开）。
	switch model.SessionStatus(detail.Status) {
	case model.StatusPending, model.StatusDeploying, model.StatusArmed,
		model.StatusRunning, model.StatusResolved:
		detail.DefectName = ""
	}
	return detail, nil
}

// ListHistory 历史分页。
func (s *SessionService) ListHistory(
	ctx context.Context, userID int64, f SessionListFilter,
) (*HistoryPage, error) {
	f.UserID = userID
	list, total, err := s.store.ListSessions(ctx, f)
	if err != nil {
		return nil, err
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	return &HistoryPage{
		List:     list,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ——— 概览 ———

// Overview 组装概览统计与双维雷达。
func (s *SessionService) Overview(ctx context.Context, userID int64) (*OverviewResponse, error) {
	metrics, err := s.store.GetMetrics(ctx, userID)
	if err != nil {
		return nil, err
	}
	scores, err := s.store.GetScoreStats(ctx, userID)
	if err != nil {
		return nil, err
	}
	techRadar, collabRadar, err := s.store.LatestScoredRadar(ctx, userID)
	if err != nil {
		return nil, err
	}

	stats := OverviewStats{
		TotalSessions:  metrics.TotalSessions,
		Finished:       metrics.Finished,
		TotalMinutes:   metrics.TotalDuration / 60,
		AvgTechScore:   scores.AvgTechScore,
		AvgCollabScore: scores.AvgCollabScore,
	}
	if metrics.Finished > 0 {
		stats.SuccessRate = metrics.Success * 100 / metrics.Finished
	}

	resp := &OverviewResponse{Stats: stats}

	// 有任意一条雷达可展示时才输出雷达块。
	if len(techRadar.Values) > 0 || len(collabRadar.Values) > 0 {
		techOverall := avgOrZero(techRadar.Values)
		if scores.AvgTechScore != nil {
			techOverall = *scores.AvgTechScore
		}
		collabOverall := avgOrZero(collabRadar.Values)
		if scores.AvgCollabScore != nil {
			collabOverall = *scores.AvgCollabScore
		}
		hasCollab := len(collabRadar.Values) > 0
		overall := techOverall
		level := CollabLevel(collabOverall)
		if hasCollab {
			overall = techOverall*0.6 + collabOverall*0.4
		}
		resp.Radar = &DualDimensionRadarDTO{
			CurrentTech:   techRadar,
			CurrentCollab: collabRadar,
			TechOverall:   techOverall,
			CollabOverall: collabOverall,
			HasCollab:     hasCollab,
			Overall:       overall,
			Level:         level,
		}
	}
	return resp, nil
}

// ——— 人机对照 ———

// Comparison 组装配对两会话的指标与双雷达。无配对返回 (nil,nil)，由 handler 给 404。
func (s *SessionService) Comparison(ctx context.Context, userID int64) (*ComparisonResponse, error) {
	manualRow, aiRow, err := s.store.GetComparisonPair(ctx, userID)
	if err != nil {
		return nil, err
	}
	if manualRow == nil || aiRow == nil {
		return nil, nil
	}
	return s.buildComparison(ctx, manualRow, aiRow)
}

// buildComparison 由一对配对行组装完整对照响应（指标 + 双雷达）。
// Comparison 与 StreamComparisonSummary 共用，保证两处给 ai-service/前端的数据一致。
func (s *SessionService) buildComparison(
	ctx context.Context, manualRow, aiRow *ComparisonSessionRow,
) (*ComparisonResponse, error) {
	manual, err := s.buildMetrics(ctx, manualRow)
	if err != nil {
		return nil, err
	}
	aiCollab, err := s.buildMetrics(ctx, aiRow)
	if err != nil {
		return nil, err
	}

	manualReview, err := s.store.GetSessionReviewRow(ctx, manualRow.ID)
	if err != nil {
		return nil, err
	}
	aiReview, err := s.store.GetSessionReviewRow(ctx, aiRow.ID)
	if err != nil {
		return nil, err
	}

	return &ComparisonResponse{
		Manual:   manual,
		AICollab: aiCollab,
		TechRadar: RadarPair{
			Manual:   techRadarOf(manualReview),
			AICollab: techRadarOf(aiReview),
		},
		CollabRadar: RadarPair{
			Manual:   CollabRadarFromReview(manualReview),
			AICollab: CollabRadarFromReview(aiReview),
		},
	}, nil
}

// StreamComparisonSummary 组装当前用户最近一对配对会话的对照数据，序列化为 JSON
// 后交给 ai-service 流式生成四段式点评。配对不存在 / streamer 未装配时返回哨兵错误，
// 由 handler 映射错误码，前端据此降级使用规则文案。
func (s *SessionService) StreamComparisonSummary(
	ctx context.Context, userID int64,
) (<-chan StreamEvent, error) {
	if s.summaryStreamer == nil {
		return nil, ErrComparisonSummaryUnavailable
	}

	manualRow, aiRow, err := s.store.GetComparisonPair(ctx, userID)
	if err != nil {
		return nil, err
	}
	if manualRow == nil || aiRow == nil {
		return nil, ErrSessionNotFound
	}

	comparison, err := s.buildComparison(ctx, manualRow, aiRow)
	if err != nil {
		return nil, err
	}

	// 给 ai-service 的 JSON 即 GET /comparison 的响应数据体，字段契约完全一致。
	raw, err := json.Marshal(comparison)
	if err != nil {
		return nil, err
	}
	return s.summaryStreamer.StreamComparisonSummary(ctx, string(raw))
}

// buildMetrics 补全会话操作指标，组装单侧 DTO。
func (s *SessionService) buildMetrics(ctx context.Context, row *ComparisonSessionRow) (*TrainingMetricsDTO, error) {
	dangerous, observe, aiQuestions, err := s.store.CountSessionOperations(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	return &TrainingMetricsDTO{
		SessionID:         row.ID,
		DefectName:        row.DefectName,
		Success:           row.Success,
		TotalTimeSec:      row.DurationSec,
		TechScore:         row.TechScore,
		CollabScore:       row.CollabScore,
		DangerousOps:      dangerous,
		VerifyActions:     observe,
		AIQuestions:       aiQuestions,
		PitfallRecognized: row.PitfallRecognized,
		PitfallTotal:      row.PitfallTotal,
	}, nil
}

func techRadarOf(row *SessionReviewRow) model.RadarData {
	if row == nil {
		return model.RadarData{}
	}
	return row.LoadTechRadar()
}

// CollabRadarFromReview 用 reviews 协作子分构造 6 维协作雷达。
// 维度顺序固定为 CollabRadarDimensions：
//   - question_quality   ← question_quality_score
//   - info_utilization   ← info_utilization_score
//   - judgment           ← judgment_score
//   - pitfall_awareness  ← error_recognition_rate
//   - verification_habit ← evidence_validation_rate
//   - independence       ← reviews 无独立来源，置 0
//
// nil 行或缺分数的维度取 0。
func CollabRadarFromReview(row *SessionReviewRow) model.RadarData {
	values := make([]float64, len(CollabRadarDimensions))
	if row != nil {
		values[0] = derefScore(row.QuestionQuality)
		values[1] = derefScore(row.InfoUtilization)
		values[2] = derefScore(row.Judgment)
		values[3] = derefScore(row.ErrorRecognitionRate)
		values[4] = derefScore(row.EvidenceValidationRate)
		// independence 暂无数据来源，保持 0。
	}
	return model.RadarData{
		Dimensions: append([]string{}, CollabRadarDimensions...),
		Values:     values,
	}
}

func derefScore(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// ——— 哨兵错误 ———

var (
	// ErrSessionNotFound 会话不存在或不属于当前用户。
	ErrSessionNotFound = errors.New("训练会话不存在")
	// ErrSessionProjectRequired 创建会话既未提供 project_id 也未提供 template_slug。
	ErrSessionProjectRequired = errors.New("必须指定训练项目（project_id 或 template_slug）")
	// ErrSessionNotStartable 当前状态不允许开始。
	ErrSessionNotStartable = errors.New("当前训练状态不允许开始")
	// ErrSessionNotFinishable 当前状态不允许结束。
	ErrSessionNotFinishable = errors.New("当前训练状态不允许结束")
	// ErrInvalidOutcome 非法结束类型。
	ErrInvalidOutcome = errors.New("非法结束类型")
	// ErrComparisonSummaryUnavailable 对照总结端口未装配（配置缺失），
	// 前端应降级使用规则文案，而非当作业务失败。
	ErrComparisonSummaryUnavailable = errors.New("对照总结服务暂不可用")
)

// ——— 辅助 ———

// NormalizeDifficulty 把前端难度归一为 DB 口径：normal→medium，非法值回落 medium。
func NormalizeDifficulty(level string) string {
	switch level {
	case "easy":
		return "easy"
	case "hard":
		return "hard"
	case "medium", "normal":
		return "medium"
	default:
		return "medium"
	}
}

// CollabLevel 协作等级阈值 85/70/55/40（C5..C2，其下 C1）。
func CollabLevel(score float64) string {
	switch {
	case score >= 85:
		return "C5"
	case score >= 70:
		return "C4"
	case score >= 55:
		return "C3"
	case score >= 40:
		return "C2"
	default:
		return "C1"
	}
}

func avgOrZero(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// NewPlannerRNG 供装配层创建确定性可控的随机源（普通 math/rand）。
func NewPlannerRNG() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

func (s *SessionService) SetBrainLifecycle(b interface {
	Freeze(context.Context, int64, string) error
}) {
	s.brain = b
}
