// Package service 分析服务。
//
// 提供事件埋点采集、漏斗分析、用户行为统计等能力。
package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/repository"
)

// AnalyticsService 分析服务。
type AnalyticsService struct {
	repo repository.EventRepository
}

// NewAnalyticsService 创建分析服务。
func NewAnalyticsService(repo repository.EventRepository) *AnalyticsService {
	return &AnalyticsService{repo: repo}
}

// TrackRequest 埋点上报请求。
type TrackRequest struct {
	EventType  string                 `json:"event_type"`
	UserID     int64                  `json:"user_id"`
	SessionID  int64                  `json:"session_id"`
	ProjectID  int64                  `json:"project_id"`
	Properties map[string]interface{} `json:"properties"`
	OccurredAt time.Time              `json:"occurred_at"`

	// 由 handler 填充
	ClientIP  string `json:"-"`
	UserAgent string `json:"-"`
	Platform  string `json:"-"`
}

// Track 上报一个事件。
// 返回是否成功记录（唯一事件重复上报返回 false, nil）。
//
// 派生规则：生产端统一上报 first_complete（无法预先知道自己是否首训完成），
// 当该唯一事件已存在时，本次完成自动补记一条 retrain，保证漏斗第四步
// 不需要额外的生产端判断。
func (s *AnalyticsService) Track(ctx context.Context, req *TrackRequest) (bool, error) {
	if req.EventType == "" {
		return false, fmt.Errorf("事件类型不能为空")
	}

	event := &model.Event{
		EventType:  req.EventType,
		UserID:     req.UserID,
		SessionID:  req.SessionID,
		ProjectID:  req.ProjectID,
		Properties: req.Properties,
		OccurredAt: req.OccurredAt,
		ClientIP:   req.ClientIP,
		UserAgent:  req.UserAgent,
		Platform:   req.Platform,
	}

	recorded, err := s.repo.Track(ctx, event)
	if err != nil {
		return false, err
	}

	if !recorded && event.EventType == model.EventFirstComplete && event.UserID > 0 {
		retrain := *event
		retrain.ID = 0
		retrain.EventType = model.EventRetrain
		if _, err := s.repo.Track(ctx, &retrain); err != nil {
			return false, fmt.Errorf("记录复训事件失败: %w", err)
		}
	}

	return recorded, nil
}

// ——— 漏斗分析 ———

// FunnelQuery 漏斗查询参数。
type FunnelQuery struct {
	FunnelCode string    // 漏斗编码
	StartDate  time.Time // 开始时间（含）
	EndDate    time.Time // 结束时间（含）
}

// AnalyzeFunnel 分析指定漏斗的转化情况。
//
// 计算逻辑：
//  1. 获取每一步的用户集合（在时间范围内触发过该事件的用户）
//  2. 每一步的用户数 = 到达该步骤的独立用户数
//  3. 转化率 = 当前步用户数 / 上一步用户数 * 100%
//  4. 流失率 = 100% - 转化率
func (s *AnalyticsService) AnalyzeFunnel(ctx context.Context, q FunnelQuery) (*model.FunnelResult, error) {
	funnel, ok := model.GetFunnel(q.FunnelCode)
	if !ok {
		return nil, fmt.Errorf("漏斗不存在: %s", q.FunnelCode)
	}

	if len(funnel.Steps) == 0 {
		return nil, fmt.Errorf("漏斗没有步骤")
	}

	// 获取每一步的用户集合
	stepUsers := make([]map[int64]bool, len(funnel.Steps))
	for i, step := range funnel.Steps {
		users, err := s.repo.GetUniqueUsers(ctx, step, q.StartDate, q.EndDate)
		if err != nil {
			return nil, fmt.Errorf("获取步骤 %s 用户失败: %w", step, err)
		}
		stepUsers[i] = users
	}

	// 计算每一步的结果
	steps := make([]model.FunnelStepResult, len(funnel.Steps))
	var firstCount int
	var lastCount int

	for i := range funnel.Steps {
		count := len(stepUsers[i])
		conversion := 0.0
		dropOff := 0.0

		if i == 0 {
			firstCount = count
			conversion = 100.0 // 第一步转化率 100%
		} else {
			prevCount := len(stepUsers[i-1])
			if prevCount > 0 {
				conversion = float64(count) / float64(prevCount) * 100
			}
			dropOff = 100.0 - conversion
		}

		if i == len(funnel.Steps)-1 {
			lastCount = count
		}

		steps[i] = model.FunnelStepResult{
			StepIndex:  i,
			EventType:  funnel.Steps[i],
			UserCount:  count,
			Conversion: roundTo2(conversion),
			DropOff:    roundTo2(dropOff),
		}
	}

	// 整体转化率
	overall := 0.0
	if firstCount > 0 {
		overall = float64(lastCount) / float64(firstCount) * 100
	}

	return &model.FunnelResult{
		FunnelCode:        funnel.Code,
		FunnelName:        funnel.Name,
		Steps:             steps,
		TotalUsers:        firstCount,
		FinalUsers:        lastCount,
		OverallConversion: roundTo2(overall),
		StartDate:         q.StartDate,
		EndDate:           q.EndDate,
	}, nil
}

// ListFunnels 返回所有内置漏斗定义。
func (s *AnalyticsService) ListFunnels() []model.Funnel {
	return model.BuiltInFunnels
}

// ——— 简单统计 ———

// EventCount 统计某事件的触发次数或用户数。
func (s *AnalyticsService) EventCount(ctx context.Context, eventType string, unique bool, start, end time.Time) (int, error) {
	if eventType == "" {
		return 0, fmt.Errorf("事件类型不能为空")
	}
	return s.repo.CountByEventType(ctx, eventType, unique, start, end)
}

// DashboardStats 首页统计卡片数据。
type DashboardStats struct {
	TotalUsers        int                 `json:"total_users"`               // 总注册用户
	ActiveUsers       int                 `json:"active_users"`              // 活跃用户（启动过沙箱）
	CompletedUsers    int                 `json:"completed_users"`           // 完成过训练的用户
	RetrainUsers      int                 `json:"retrain_users"`             // 复训用户数
	OverallConversion float64             `json:"overall_conversion"`        // 整体转化率（注册→完成）
	TrainingFunnel    *model.FunnelResult `json:"training_funnel,omitempty"` // 训练漏斗
}

// GetDashboardStats 获取看板首页统计数据。
func (s *AnalyticsService) GetDashboardStats(ctx context.Context, start, end time.Time) (*DashboardStats, error) {
	// 各步骤用户数
	registerUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventRegister, start, end)
	startUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventFirstStart, start, end)
	completeUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventFirstComplete, start, end)
	retrainUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventRetrain, start, end)

	overall := 0.0
	if len(registerUsers) > 0 {
		overall = float64(len(completeUsers)) / float64(len(registerUsers)) * 100
	}

	// 同时计算完整漏斗
	funnel, err := s.AnalyzeFunnel(ctx, FunnelQuery{
		FunnelCode: "training_funnel",
		StartDate:  start,
		EndDate:    end,
	})
	if err != nil {
		// 漏斗计算失败不影响主统计
		funnel = nil
	}

	return &DashboardStats{
		TotalUsers:        len(registerUsers),
		ActiveUsers:       len(startUsers),
		CompletedUsers:    len(completeUsers),
		RetrainUsers:      len(retrainUsers),
		OverallConversion: roundTo2(overall),
		TrainingFunnel:    funnel,
	}, nil
}

// ListEvents 分页查询事件列表（调试/审计用）。
func (s *AnalyticsService) ListEvents(ctx context.Context, userID int64, eventType string, page, pageSize int) ([]model.Event, int, error) {
	return s.repo.ListEvents(ctx, userID, eventType, page, pageSize)
}

// ——— AI 行为统计（T4-14）———

// AIBehaviorStats AI 行为统计概览。
type AIBehaviorStats struct {
	// 复盘相关
	ReviewViews     int `json:"review_views"`      // 复盘查看次数
	ReviewShares    int `json:"review_shares"`     // 复盘分享次数
	ReviewViewUsers int `json:"review_view_users"` // 查看复盘的独立用户数

	// 协作评估相关
	CollabViews     int `json:"collab_views"`      // 协作评估查看次数
	CollabEvaluates int `json:"collab_evaluates"`  // 协作评估提交数
	CollabComments  int `json:"collab_comments"`   // 协作评论数
	CollabViewUsers int `json:"collab_view_users"` // 查看协作评估的独立用户数

	// 坑点相关
	PitfallEncounters int `json:"pitfall_encounters"` // 遇到坑点次数
	PitfallRecognizes int `json:"pitfall_recognizes"` // 识别坑点次数
	PitfallFalls      int `json:"pitfall_falls"`      // 踩坑次数
	PitfallUsers      int `json:"pitfall_users"`      // 受坑点影响的独立用户数

	// AI 对话相关
	AIChatMessages int `json:"ai_chat_messages"` // AI 对话消息数
	AIHintRequests int `json:"ai_hint_requests"` // AI 提示请求数
	AIExplanations int `json:"ai_explanations"`  // AI 讲解请求数
	AIActiveUsers  int `json:"ai_active_users"`  // 使用 AI 功能的独立用户数

	// 总览
	TotalAIEvents int `json:"total_ai_events"` // AI 相关事件总数
}

// GetAIBehaviorStats 获取 AI 行为统计。
// 统计指定时间范围内所有 AI 相关行为的数据。
func (s *AnalyticsService) GetAIBehaviorStats(ctx context.Context, start, end time.Time) (*AIBehaviorStats, error) {
	stats := &AIBehaviorStats{}

	// ——— 复盘相关 ———
	stats.ReviewViews, _ = s.repo.CountByEventType(ctx, model.EventReviewView, false, start, end)
	stats.ReviewShares, _ = s.repo.CountByEventType(ctx, model.EventReviewShare, false, start, end)
	reviewUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventReviewView, start, end)
	stats.ReviewViewUsers = len(reviewUsers)

	// ——— 协作评估相关 ———
	stats.CollabViews, _ = s.repo.CountByEventType(ctx, model.EventCollabView, false, start, end)
	stats.CollabEvaluates, _ = s.repo.CountByEventType(ctx, model.EventCollabEvaluate, false, start, end)
	stats.CollabComments, _ = s.repo.CountByEventType(ctx, model.EventCollabComment, false, start, end)
	collabUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventCollabView, start, end)
	stats.CollabViewUsers = len(collabUsers)

	// ——— 坑点相关 ———
	stats.PitfallEncounters, _ = s.repo.CountByEventType(ctx, model.EventPitfallEncounter, false, start, end)
	stats.PitfallRecognizes, _ = s.repo.CountByEventType(ctx, model.EventPitfallRecognize, false, start, end)
	stats.PitfallFalls, _ = s.repo.CountByEventType(ctx, model.EventPitfallFall, false, start, end)
	// 受坑点影响的用户 = 遇到坑点的用户
	pitfallUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventPitfallEncounter, start, end)
	stats.PitfallUsers = len(pitfallUsers)

	// ——— AI 对话相关 ———
	stats.AIChatMessages, _ = s.repo.CountByEventType(ctx, model.EventAIChatMessage, false, start, end)
	stats.AIHintRequests, _ = s.repo.CountByEventType(ctx, model.EventAIHintRequest, false, start, end)
	stats.AIExplanations, _ = s.repo.CountByEventType(ctx, model.EventAIExplanation, false, start, end)

	// AI 活跃用户 = 使用过任意 AI 功能的用户
	aiUsers := make(map[int64]bool)
	for _, evt := range []string{
		model.EventAIChatMessage,
		model.EventAIHintRequest,
		model.EventAIExplanation,
		model.EventReviewView,
		model.EventPitfallEncounter,
	} {
		users, _ := s.repo.GetUniqueUsers(ctx, evt, start, end)
		for uid := range users {
			aiUsers[uid] = true
		}
	}
	stats.AIActiveUsers = len(aiUsers)

	// 总事件数
	total := 0
	for _, evt := range model.AIBehaviorEvents {
		count, _ := s.repo.CountByEventType(ctx, evt, false, start, end)
		total += count
	}
	stats.TotalAIEvents = total

	return stats, nil
}

// PitfallConversion 坑点转化漏斗。
// 遇到坑点 → 识别坑点 → 踩坑（受影响）
type PitfallConversion struct {
	EncounterUsers  int     `json:"encounter_users"`  // 遇到坑点用户数
	RecognizeUsers  int     `json:"recognize_users"`  // 识别坑点用户数
	FallUsers       int     `json:"fall_users"`       // 踩坑用户数
	RecognitionRate float64 `json:"recognition_rate"` // 识别率（%）
	FallRate        float64 `json:"fall_rate"`        // 踩坑率（%）
}

// GetPitfallConversion 获取坑点转化漏斗数据。
func (s *AnalyticsService) GetPitfallConversion(ctx context.Context, start, end time.Time) (*PitfallConversion, error) {
	encounterUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventPitfallEncounter, start, end)
	recognizeUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventPitfallRecognize, start, end)
	fallUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventPitfallFall, start, end)

	encounterCount := len(encounterUsers)
	recognizeCount := len(recognizeUsers)
	fallCount := len(fallUsers)

	recognitionRate := 0.0
	if encounterCount > 0 {
		recognitionRate = float64(recognizeCount) / float64(encounterCount) * 100
	}

	fallRate := 0.0
	if encounterCount > 0 {
		fallRate = float64(fallCount) / float64(encounterCount) * 100
	}

	return &PitfallConversion{
		EncounterUsers:  encounterCount,
		RecognizeUsers:  recognizeCount,
		FallUsers:       fallCount,
		RecognitionRate: roundTo2(recognitionRate),
		FallRate:        roundTo2(fallRate),
	}, nil
}

// AIEventsBreakdown AI 事件分类明细。
type AIEventsBreakdown struct {
	Items []EventCountItem `json:"items"`
	Total int              `json:"total"`
}

// EventCountItem 单类事件计数。
type EventCountItem struct {
	EventType   string `json:"event_type"`
	EventName   string `json:"event_name"`
	Count       int    `json:"count"`
	UniqueUsers int    `json:"unique_users"`
}

// GetAIEventsBreakdown 获取 AI 事件分类明细。
func (s *AnalyticsService) GetAIEventsBreakdown(ctx context.Context, start, end time.Time) (*AIEventsBreakdown, error) {
	eventNames := map[string]string{
		model.EventReviewView:       "查看复盘",
		model.EventReviewShare:      "分享复盘",
		model.EventCollabView:       "查看协作评估",
		model.EventCollabEvaluate:   "提交协作评估",
		model.EventCollabComment:    "协作评论",
		model.EventPitfallEncounter: "遇到坑点",
		model.EventPitfallRecognize: "识别坑点",
		model.EventPitfallFall:      "踩坑",
		model.EventAIChatMessage:    "AI对话",
		model.EventAIHintRequest:    "AI提示",
		model.EventAIExplanation:    "AI讲解",
	}

	items := make([]EventCountItem, 0, len(model.AIBehaviorEvents))
	total := 0

	for _, evt := range model.AIBehaviorEvents {
		count, _ := s.repo.CountByEventType(ctx, evt, false, start, end)
		users, _ := s.repo.GetUniqueUsers(ctx, evt, start, end)
		name := eventNames[evt]
		if name == "" {
			name = evt
		}
		items = append(items, EventCountItem{
			EventType:   evt,
			EventName:   name,
			Count:       count,
			UniqueUsers: len(users),
		})
		total += count
	}

	return &AIEventsBreakdown{
		Items: items,
		Total: total,
	}, nil
}

// ——— 工具函数 ———

// roundTo2 保留两位小数。math.Round 对负数也按四舍五入处理
// （旧实现 int(v*100+0.5) 对负数会截断错档，如 -15.00 被算成 -14.99）。
func roundTo2(v float64) float64 {
	return math.Round(v*100) / 100
}

// ——— 排训指标看板（T5-7）———

// TrainingMetrics 排训核心指标。
type TrainingMetrics struct {
	// —— 转化类 ——
	FirstStartUsers     int     `json:"first_start_users"`     // 首次启动用户数
	FirstCompleteUsers  int     `json:"first_complete_users"`  // 首次完成用户数
	FirstCompletionRate float64 `json:"first_completion_rate"` // 首训完成率（%）
	RetrainUsers        int     `json:"retrain_users"`         // 复训用户数
	RetrainRate         float64 `json:"retrain_rate"`          // 复训率（%）

	// —— 质量类 ——
	ReviewRatings      int     `json:"review_ratings"`      // 复盘评价总数
	PositiveRatings    int     `json:"positive_ratings"`    // 好评数（score >= 4）
	ReviewSatisfaction float64 `json:"review_satisfaction"` // 复盘好评率（%）

	// —— 效率类 ——
	AvgTrainingDuration    float64 `json:"avg_training_duration"`     // 平均训练时长（秒）
	AvgTrainingDurationMin float64 `json:"avg_training_duration_min"` // 平均训练时长（分钟）

	// —— 总量 ——
	TotalSessions  int     `json:"total_sessions"`  // 总训练场次
	TotalCompleted int     `json:"total_completed"` // 总完成场次
	CompletionRate float64 `json:"completion_rate"` // 整体完成率（%）
}

// GetTrainingMetrics 获取排训核心指标。
//
// 指标口径：
//   - 首训完成率 = 首次完成用户数 / 首次启动用户数 × 100%
//   - 复训率 = 复训用户数 / 首次完成用户数 × 100%
//   - 复盘好评率 = 评分>=4的评价数 / 总评价数 × 100%
//   - 平均训练时长 = 所有完成训练的时长平均值
func (s *AnalyticsService) GetTrainingMetrics(ctx context.Context, start, end time.Time) (*TrainingMetrics, error) {
	m := &TrainingMetrics{}

	// —— 转化类：首训漏斗 ——
	firstStartUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventFirstStart, start, end)
	firstCompleteUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventFirstComplete, start, end)
	retrainUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventRetrain, start, end)

	m.FirstStartUsers = len(firstStartUsers)
	m.FirstCompleteUsers = len(firstCompleteUsers)
	m.RetrainUsers = len(retrainUsers)

	if m.FirstStartUsers > 0 {
		m.FirstCompletionRate = roundTo2(float64(m.FirstCompleteUsers) / float64(m.FirstStartUsers) * 100)
	}
	if m.FirstCompleteUsers > 0 {
		m.RetrainRate = roundTo2(float64(m.RetrainUsers) / float64(m.FirstCompleteUsers) * 100)
	}

	// —— 质量类：复盘好评 ——
	totalRatings, _ := s.repo.CountByEventType(ctx, model.EventReviewRating, false, start, end)
	positiveRatings, _ := s.repo.CountByPropertyValue(ctx, model.EventReviewRating, "score", "gte", 4.0, start, end)

	m.ReviewRatings = totalRatings
	m.PositiveRatings = positiveRatings
	if m.ReviewRatings > 0 {
		m.ReviewSatisfaction = roundTo2(float64(m.PositiveRatings) / float64(m.ReviewRatings) * 100)
	}

	// —— 效率类：平均训练时长 ——
	avgDuration, _ := s.repo.GetAverageNumericProperty(ctx, model.EventTrainingComplete, "duration_seconds", start, end)
	m.AvgTrainingDuration = roundTo2(avgDuration)
	m.AvgTrainingDurationMin = roundTo2(avgDuration / 60.0)

	// —— 总量 ——
	totalSessions, _ := s.repo.CountByEventType(ctx, model.EventStartTraining, false, start, end)
	totalCompleted, _ := s.repo.CountByEventType(ctx, model.EventTrainingComplete, false, start, end)

	m.TotalSessions = totalSessions
	m.TotalCompleted = totalCompleted
	if m.TotalSessions > 0 {
		m.CompletionRate = roundTo2(float64(m.TotalCompleted) / float64(m.TotalSessions) * 100)
	}

	return m, nil
}

// DailyMetric 单日指标。
type DailyMetric struct {
	Date               string  `json:"date"`                 // 日期 YYYY-MM-DD
	FirstStartUsers    int     `json:"first_start_users"`    // 首启用户
	FirstCompleteUsers int     `json:"first_complete_users"` // 首完用户
	CompletionRate     float64 `json:"completion_rate"`      // 完成率（%）
	TotalSessions      int     `json:"total_sessions"`       // 训练场次
	AvgDuration        float64 `json:"avg_duration"`         // 平均时长（分钟）
}

// GetDailyTrends 获取按天的指标趋势。
// 返回指定时间范围内每天的数据点，用于绘制趋势图。
func (s *AnalyticsService) GetDailyTrends(ctx context.Context, start, end time.Time) ([]DailyMetric, error) {
	if start.IsZero() || end.IsZero() {
		return []DailyMetric{}, nil
	}

	// 按天切分。end 为排他边界（与 handler parseDateRange 当天 +24h 的约定一致），
	// 故用 Before 而非 !After，避免把结束日 00:00 多算一个空桶。
	var days []time.Time
	d := start
	for d.Before(end) {
		days = append(days, d)
		d = d.AddDate(0, 0, 1)
	}

	result := make([]DailyMetric, 0, len(days))

	for _, day := range days {
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		dayEnd := dayStart.AddDate(0, 0, 1)

		startUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventFirstStart, dayStart, dayEnd)
		completeUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventFirstComplete, dayStart, dayEnd)
		sessions, _ := s.repo.CountByEventType(ctx, model.EventStartTraining, false, dayStart, dayEnd)
		avgDur, _ := s.repo.GetAverageNumericProperty(ctx, model.EventTrainingComplete, "duration_seconds", dayStart, dayEnd)

		completionRate := 0.0
		if len(startUsers) > 0 {
			completionRate = roundTo2(float64(len(completeUsers)) / float64(len(startUsers)) * 100)
		}

		result = append(result, DailyMetric{
			Date:               day.Format("2006-01-02"),
			FirstStartUsers:    len(startUsers),
			FirstCompleteUsers: len(completeUsers),
			CompletionRate:     completionRate,
			TotalSessions:      sessions,
			AvgDuration:        roundTo2(avgDur / 60.0),
		})
	}

	return result, nil
}

// ProjectMetric 项目维度指标。
type ProjectMetric struct {
	ProjectID      int64   `json:"project_id"`
	ProjectName    string  `json:"project_name"`
	StartUsers     int     `json:"start_users"`     // 启动用户数
	CompleteUsers  int     `json:"complete_users"`  // 完成用户数
	CompletionRate float64 `json:"completion_rate"` // 完成率（%）
	AvgDuration    float64 `json:"avg_duration"`    // 平均时长（分钟）
}

// GetProjectRankings 获取项目排行榜（按启动用户数排序）。
// 用于看板展示热门项目和各项目的完成情况。
func (s *AnalyticsService) GetProjectRankings(ctx context.Context, start, end time.Time, topN int) ([]ProjectMetric, error) {
	// 从事件中收集所有涉及的 project_id
	allProjects := make(map[int64]bool)
	// 用内存仓储的话需要遍历所有事件；这里提供通用实现
	// 注：实际 PG 实现可用 GROUP BY project_id 优化

	// 收集启动过的项目
	startEvents, total, err := s.repo.ListEvents(ctx, 0, model.EventStartTraining, 1, 1000)
	if err != nil {
		return nil, err
	}
	_ = total

	for _, e := range startEvents {
		if e.ProjectID > 0 {
			allProjects[e.ProjectID] = true
		}
	}

	// 对每个项目统计
	rankings := make([]ProjectMetric, 0, len(allProjects))
	for pid := range allProjects {
		// 启动用户数（该项目首启用户）
		// 简化实现：用 start_training 事件的独立用户数近似
		startUsers := 0
		completeUsers := 0
		var totalDur float64
		durCount := 0

		// 遍历所有事件统计（内存版实现方式）
		// 注：生产环境 PG 版会用 SQL 优化
		events, _, _ := s.repo.ListEvents(ctx, 0, "", 1, 10000)
		startUserSet := make(map[int64]bool)
		completeUserSet := make(map[int64]bool)

		for _, e := range events {
			if e.ProjectID != pid {
				continue
			}
			if !start.IsZero() && e.OccurredAt.Before(start) {
				continue
			}
			if !end.IsZero() && e.OccurredAt.After(end) {
				continue
			}

			switch e.EventType {
			case model.EventStartTraining:
				if e.UserID > 0 {
					startUserSet[e.UserID] = true
				}
			case model.EventTrainingComplete:
				if e.UserID > 0 {
					completeUserSet[e.UserID] = true
				}
				if e.Properties != nil {
					if dur, ok := e.Properties["duration_seconds"]; ok {
						switch v := dur.(type) {
						case float64:
							totalDur += v
							durCount++
						case int:
							totalDur += float64(v)
							durCount++
						}
					}
				}
			}
		}

		startUsers = len(startUserSet)
		completeUsers = len(completeUserSet)

		completionRate := 0.0
		if startUsers > 0 {
			completionRate = roundTo2(float64(completeUsers) / float64(startUsers) * 100)
		}

		avgDur := 0.0
		if durCount > 0 {
			avgDur = roundTo2(totalDur / float64(durCount) / 60.0)
		}

		rankings = append(rankings, ProjectMetric{
			ProjectID:      pid,
			ProjectName:    "", // 项目名由前端补全或从 content-service 查询
			StartUsers:     startUsers,
			CompleteUsers:  completeUsers,
			CompletionRate: completionRate,
			AvgDuration:    avgDur,
		})
	}

	// 按启动用户数降序排序
	for i := 0; i < len(rankings); i++ {
		for j := i + 1; j < len(rankings); j++ {
			if rankings[j].StartUsers > rankings[i].StartUsers {
				rankings[i], rankings[j] = rankings[j], rankings[i]
			}
		}
	}

	// 取 Top N
	if topN > 0 && len(rankings) > topN {
		rankings = rankings[:topN]
	}

	return rankings, nil
}

// ——— AI 专项看板（T5-8）———

// AISpecialtyMetrics AI 专项指标。
type AISpecialtyMetrics struct {
	// —— 坑点识别 ——
	PitfallEncounterUsers int     `json:"pitfall_encounter_users"` // 遇到坑点用户数
	PitfallRecognizeUsers int     `json:"pitfall_recognize_users"` // 识别坑点用户数
	RecognitionRate       float64 `json:"recognition_rate"`        // 识别率（%）

	// —— 证据验证 ——
	AIActiveUsers    int     `json:"ai_active_users"`   // AI 活跃用户数
	VerifyUsers      int     `json:"verify_users"`      // 有验证行为的用户数
	VerificationRate float64 `json:"verification_rate"` // 验证率（%）

	// —— 协作查看 ——
	CompletedUsers  int     `json:"completed_users"`   // 完成训练用户数
	CollabViewUsers int     `json:"collab_view_users"` // 查看协作评估的用户数
	CollabViewRate  float64 `json:"collab_view_rate"`  // 协作查看率（%）

	// —— 危险操作 ——
	TotalDangerousOps      int     `json:"total_dangerous_ops"`        // 危险操作总数
	AvgDangerousOpsPerUser float64 `json:"avg_dangerous_ops_per_user"` // 人均危险操作数

	// —— 人机对照 ——
	ABPairCount       int     `json:"ab_pair_count"`        // 完成的对照组数
	ABSpeedDiff       float64 `json:"ab_speed_diff"`        // 速度差（AI-人工，负数=AI更快）
	ABAccuracyDiff    float64 `json:"ab_accuracy_diff"`     // 正确率差（%，AI-人工）
	ABDangerousOpDiff float64 `json:"ab_dangerous_op_diff"` // 危险操作率差（%，AI-人工）
	ABVerifyDiff      float64 `json:"ab_verify_diff"`       // 验证行为差（%，AI-人工）

	// —— 样本量 ——
	SampleInfo SampleInfo `json:"sample_info"` // 样本量信息
}

// SampleInfo 样本量信息与提示。
type SampleInfo struct {
	TotalUsers        int    `json:"total_users"`        // 总用户数
	ABPairCount       int    `json:"ab_pair_count"`      // 对照组数
	EnoughUsers       bool   `json:"enough_users"`       // 用户数是否足够（≥50）
	EnoughAB          bool   `json:"enough_ab"`          // 对照数是否足够（≥30）
	UserTip           string `json:"user_tip"`           // 用户数提示文案
	ABTip             string `json:"ab_tip"`             // 对照数提示文案
	OverallConfidence string `json:"overall_confidence"` // 整体置信度：高/中/低
}

// GetAISpecialtyMetrics 获取 AI 专项指标。
//
// 涵盖识别率、验证率、协作查看率、危险操作、人机对照等核心指标。
// 并提供样本量提示，数据不足时给出明确提示。
func (s *AnalyticsService) GetAISpecialtyMetrics(ctx context.Context, start, end time.Time) (*AISpecialtyMetrics, error) {
	m := &AISpecialtyMetrics{}

	// —— 坑点识别率 ——
	encounterUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventPitfallEncounter, start, end)
	recognizeUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventPitfallRecognize, start, end)

	m.PitfallEncounterUsers = len(encounterUsers)
	m.PitfallRecognizeUsers = len(recognizeUsers)
	if m.PitfallEncounterUsers > 0 {
		m.RecognitionRate = roundTo2(float64(m.PitfallRecognizeUsers) / float64(m.PitfallEncounterUsers) * 100)
	}

	// —— 验证率 ——
	// AI 活跃用户（使用过任意 AI 功能）
	aiUsers := make(map[int64]bool)
	for _, evt := range []string{
		model.EventAIChatMessage,
		model.EventAIHintRequest,
		model.EventAIExplanation,
		model.EventPitfallEncounter,
		model.EventReviewView,
	} {
		users, _ := s.repo.GetUniqueUsers(ctx, evt, start, end)
		for uid := range users {
			aiUsers[uid] = true
		}
	}
	m.AIActiveUsers = len(aiUsers)

	// 有验证行为的用户
	verifyUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventEvidenceVerify, start, end)
	m.VerifyUsers = len(verifyUsers)
	if m.AIActiveUsers > 0 {
		m.VerificationRate = roundTo2(float64(m.VerifyUsers) / float64(m.AIActiveUsers) * 100)
	}

	// —— 协作查看率 ——
	completedUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventFirstComplete, start, end)
	collabViewUsers, _ := s.repo.GetUniqueUsers(ctx, model.EventCollabView, start, end)

	m.CompletedUsers = len(completedUsers)
	m.CollabViewUsers = len(collabViewUsers)
	if m.CompletedUsers > 0 {
		m.CollabViewRate = roundTo2(float64(m.CollabViewUsers) / float64(m.CompletedUsers) * 100)
	}

	// —— 危险操作 ——
	dangerousOps, _ := s.repo.CountByEventType(ctx, model.EventDangerousOp, false, start, end)
	m.TotalDangerousOps = dangerousOps
	if m.AIActiveUsers > 0 {
		m.AvgDangerousOpsPerUser = roundTo2(float64(dangerousOps) / float64(m.AIActiveUsers))
	}

	// —— 人机对照 A/B ——
	abPairs, _ := s.repo.CountByEventType(ctx, model.EventABComplete, false, start, end)
	m.ABPairCount = abPairs

	// 对照指标从事件属性中统计（如果有数据的话）
	// 速度差、正确率差等存储在 ab_complete 事件的 properties 中
	if abPairs > 0 {
		avgSpeedDiff, _ := s.repo.GetAverageNumericProperty(ctx, model.EventABComplete, "speed_diff_pct", start, end)
		avgAccDiff, _ := s.repo.GetAverageNumericProperty(ctx, model.EventABComplete, "accuracy_diff", start, end)
		avgDangerDiff, _ := s.repo.GetAverageNumericProperty(ctx, model.EventABComplete, "dangerous_op_diff_pct", start, end)
		avgVerifyDiff, _ := s.repo.GetAverageNumericProperty(ctx, model.EventABComplete, "verify_diff_pct", start, end)

		m.ABSpeedDiff = roundTo2(avgSpeedDiff)
		m.ABAccuracyDiff = roundTo2(avgAccDiff)
		m.ABDangerousOpDiff = roundTo2(avgDangerDiff)
		m.ABVerifyDiff = roundTo2(avgVerifyDiff)
	}

	// —— 样本量提示 ——
	m.SampleInfo = s.calcSampleInfo(m.AIActiveUsers, abPairs)

	return m, nil
}

// calcSampleInfo 计算样本量信息和提示文案。
//
// 规则：
//   - 用户数 ≥ 50 → 足够，< 50 → 不足
//   - 对照数 ≥ 30 → 足够，< 30 → 不足
//   - 整体置信度：都足够=高，一个不足=中，都不足=低
func (s *AnalyticsService) calcSampleInfo(totalUsers int, abPairs int) SampleInfo {
	info := SampleInfo{
		TotalUsers:  totalUsers,
		ABPairCount: abPairs,
	}

	// 用户数
	if totalUsers >= 50 {
		info.EnoughUsers = true
		info.UserTip = "样本量充足，指标具有统计意义"
	} else {
		info.EnoughUsers = false
		info.UserTip = "样本量不足（<50用户），指标仅供参考"
	}

	// 对照数
	if abPairs >= 30 {
		info.EnoughAB = true
		info.ABTip = "对照样本充足，A/B 差异具有统计意义"
	} else {
		info.EnoughAB = false
		info.ABTip = "对照样本不足（<30组），A/B 差异仅供参考"
	}

	// 整体置信度
	if info.EnoughUsers && info.EnoughAB {
		info.OverallConfidence = "高"
	} else if info.EnoughUsers || info.EnoughAB {
		info.OverallConfidence = "中"
	} else {
		info.OverallConfidence = "低"
	}

	return info
}

// AIBreakdownItem AI 能力使用明细。
type AIBreakdownItem struct {
	Category   string  `json:"category"`    // 分类
	Feature    string  `json:"feature"`     // 功能名称
	EventType  string  `json:"event_type"`  // 事件类型
	UserCount  int     `json:"user_count"`  // 使用用户数
	TotalCount int     `json:"total_count"` // 总次数
	UsageRate  float64 `json:"usage_rate"`  // 使用率（%，相对AI活跃用户）
}

// GetAIFeatureBreakdown 获取 AI 功能使用明细。
// 按功能维度拆解 AI 各能力的使用情况，用于看板展示。
func (s *AnalyticsService) GetAIFeatureBreakdown(ctx context.Context, start, end time.Time) ([]AIBreakdownItem, error) {
	// AI 活跃用户总数
	aiUsers := make(map[int64]bool)
	for _, evt := range []string{
		model.EventAIChatMessage,
		model.EventAIHintRequest,
		model.EventAIExplanation,
		model.EventReviewView,
		model.EventPitfallEncounter,
		model.EventCollabView,
		model.EventEvidenceVerify,
	} {
		users, _ := s.repo.GetUniqueUsers(ctx, evt, start, end)
		for uid := range users {
			aiUsers[uid] = true
		}
	}
	totalAIUsers := len(aiUsers)

	features := []struct {
		category  string
		feature   string
		eventType string
	}{
		{"AI对话", "AI对话", model.EventAIChatMessage},
		{"AI对话", "AI提示", model.EventAIHintRequest},
		{"AI对话", "AI讲解", model.EventAIExplanation},
		{"复盘", "查看复盘", model.EventReviewView},
		{"坑点", "遇到坑点", model.EventPitfallEncounter},
		{"坑点", "识别坑点", model.EventPitfallRecognize},
		{"协作", "查看协作评估", model.EventCollabView},
		{"验证", "证据验证", model.EventEvidenceVerify},
	}

	items := make([]AIBreakdownItem, 0, len(features))

	for _, f := range features {
		count, _ := s.repo.CountByEventType(ctx, f.eventType, false, start, end)
		users, _ := s.repo.GetUniqueUsers(ctx, f.eventType, start, end)

		usageRate := 0.0
		if totalAIUsers > 0 {
			usageRate = roundTo2(float64(len(users)) / float64(totalAIUsers) * 100)
		}

		items = append(items, AIBreakdownItem{
			Category:   f.category,
			Feature:    f.feature,
			EventType:  f.eventType,
			UserCount:  len(users),
			TotalCount: count,
			UsageRate:  usageRate,
		})
	}

	return items, nil
}
