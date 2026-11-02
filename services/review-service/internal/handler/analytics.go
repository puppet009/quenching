// Package handler 分析服务 HTTP 接口。
//
// 接口清单：
//
//	POST   /api/v1/events/track               埋点上报
//	GET    /api/v1/events                     事件列表（查询/审计）
//	GET    /api/v1/funnels                    漏斗列表
//	GET    /api/v1/funnels/:code/analyze      漏斗分析
//	GET    /api/v1/dashboard/stats            看板统计概览
//	GET    /api/v1/dashboard/training-metrics 排训核心指标（T5-7）
//	GET    /api/v1/dashboard/trends           指标日趋势（T5-7）
//	GET    /api/v1/dashboard/project-rankings 项目排行榜（T5-7）
//	GET    /api/v1/ai-behavior/stats             AI 行为统计概览
//	GET    /api/v1/ai-behavior/breakdown         AI 事件分类明细
//	GET    /api/v1/ai-behavior/pitfall-conversion 坑点转化漏斗
//	GET    /api/v1/ai-board/metrics              AI 专项指标（T5-8）
//	GET    /api/v1/ai-board/feature-breakdown    AI 功能使用明细（T5-8）
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/review-service/internal/service"
)

// AnalyticsHandler 分析服务接口处理器。
type AnalyticsHandler struct {
	svc *service.AnalyticsService
}

// NewAnalyticsHandler 创建处理器。
func NewAnalyticsHandler(svc *service.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{svc: svc}
}

// RegisterRoutes 注册路由。
func (h *AnalyticsHandler) RegisterRoutes(r *gin.Engine) {
	// 埋点采集
	events := r.Group("/api/v1/events")
	{
		events.POST("/track", h.track)
		events.GET("", h.listEvents)
	}

	// 漏斗分析
	funnels := r.Group("/api/v1/funnels")
	{
		funnels.GET("", h.listFunnels)
		funnels.GET("/:code/analyze", h.analyzeFunnel)
	}

	// 看板
	dashboard := r.Group("/api/v1/dashboard")
	{
		dashboard.GET("/stats", h.dashboardStats)
		dashboard.GET("/training-metrics", h.trainingMetrics)
		dashboard.GET("/trends", h.dailyTrends)
		dashboard.GET("/project-rankings", h.projectRankings)
	}

	// AI 行为统计
	ai := r.Group("/api/v1/ai-behavior")
	{
		ai.GET("/stats", h.aiBehaviorStats)
		ai.GET("/breakdown", h.aiEventsBreakdown)
		ai.GET("/pitfall-conversion", h.pitfallConversion)
	}

	// AI 专项看板（T5-8）
	aiBoard := r.Group("/api/v1/ai-board")
	{
		aiBoard.GET("/metrics", h.aiSpecialtyMetrics)
		aiBoard.GET("/feature-breakdown", h.aiFeatureBreakdown)
	}
}

// ——— 埋点上报 ———

// track 上报事件。
//
// 前端/其他服务调用此接口上报用户行为事件。
// 对于唯一事件（register/first_start 等），重复上报自动忽略。
func (h *AnalyticsHandler) track(c *gin.Context) {
	var req service.TrackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误: " + err.Error()})
		return
	}

	// 从请求头提取客户端信息
	req.ClientIP = getClientIP(c)
	req.UserAgent = c.GetHeader("User-Agent")
	req.Platform = c.GetHeader("X-Platform")

	// 从 JWT 或 header 获取用户 ID（如果有）
	if req.UserID == 0 {
		if uidStr := c.GetHeader("X-User-ID"); uidStr != "" {
			req.UserID, _ = strconv.ParseInt(uidStr, 10, 64)
		}
	}

	recorded, err := h.svc.Track(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":     0,
		"msg":      "ok",
		"recorded": recorded, // true=新记录, false=重复（唯一事件）
	})
}

// listEvents 事件列表（调试/审计用）。
//
// Query 参数：
//   - user_id     按用户筛选
//   - event_type  按事件类型筛选
//   - page        页码
//   - page_size   每页条数
func (h *AnalyticsHandler) listEvents(c *gin.Context) {
	var userID int64
	if v := c.Query("user_id"); v != "" {
		userID, _ = strconv.ParseInt(v, 10, 64)
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	events, total, err := h.svc.ListEvents(c.Request.Context(), userID, c.Query("event_type"), page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"items":     events,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// ——— 漏斗分析 ———

// listFunnels 返回所有可用漏斗定义。
func (h *AnalyticsHandler) listFunnels(c *gin.Context) {
	funnels := h.svc.ListFunnels()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": funnels,
	})
}

// analyzeFunnel 分析指定漏斗的转化情况。
//
// Query 参数：
//   - start_date  开始日期（YYYY-MM-DD），默认 30 天前
//   - end_date    结束日期（YYYY-MM-DD），默认今天
func (h *AnalyticsHandler) analyzeFunnel(c *gin.Context) {
	code := c.Param("code")

	start, end := parseDateRange(c)

	result, err := h.svc.AnalyzeFunnel(c.Request.Context(), service.FunnelQuery{
		FunnelCode: code,
		StartDate:  start,
		EndDate:    end,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// ——— 看板统计 ———

// dashboardStats 看板首页统计数据。
//
// Query 参数：
//   - start_date  开始日期
//   - end_date    结束日期
func (h *AnalyticsHandler) dashboardStats(c *gin.Context) {
	start, end := parseDateRange(c)

	stats, err := h.svc.GetDashboardStats(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": stats,
	})
}

// trainingMetrics 排训核心指标（T5-7）。
//
// 包含：首训完成率、复训率、复盘好评率、平均训练时长等核心指标。
//
// Query 参数：
//   - start_date  开始日期
//   - end_date    结束日期
func (h *AnalyticsHandler) trainingMetrics(c *gin.Context) {
	start, end := parseDateRange(c)

	metrics, err := h.svc.GetTrainingMetrics(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": metrics,
	})
}

// dailyTrends 按天的指标趋势（T5-7）。
//
// 返回指定时间范围内每天的指标数据点，用于绘制趋势图。
//
// Query 参数：
//   - start_date  开始日期
//   - end_date    结束日期
func (h *AnalyticsHandler) dailyTrends(c *gin.Context) {
	start, end := parseDateRange(c)

	trends, err := h.svc.GetDailyTrends(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": trends,
	})
}

// projectRankings 项目排行榜（T5-7）。
//
// 按启动用户数排序的热门项目及其完成情况。
//
// Query 参数：
//   - start_date  开始日期
//   - end_date    结束日期
//   - top         返回前 N 个，默认 10
func (h *AnalyticsHandler) projectRankings(c *gin.Context) {
	start, end := parseDateRange(c)

	top := 10
	if v := c.Query("top"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			top = n
		}
	}

	rankings, err := h.svc.GetProjectRankings(c.Request.Context(), start, end, top)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": rankings,
	})
}

// ——— AI 行为统计 ———

// aiBehaviorStats AI 行为统计概览。
//
// Query 参数：
//   - start_date  开始日期
//   - end_date    结束日期
func (h *AnalyticsHandler) aiBehaviorStats(c *gin.Context) {
	start, end := parseDateRange(c)

	stats, err := h.svc.GetAIBehaviorStats(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": stats,
	})
}

// aiEventsBreakdown AI 事件分类明细。
func (h *AnalyticsHandler) aiEventsBreakdown(c *gin.Context) {
	start, end := parseDateRange(c)

	breakdown, err := h.svc.GetAIEventsBreakdown(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": breakdown,
	})
}

// pitfallConversion 坑点转化漏斗。
func (h *AnalyticsHandler) pitfallConversion(c *gin.Context) {
	start, end := parseDateRange(c)

	conv, err := h.svc.GetPitfallConversion(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": conv,
	})
}

// ——— AI 专项看板（T5-8）———

// aiSpecialtyMetrics AI 专项指标（T5-8）。
//
// 包含识别率、验证率、协作查看率、危险操作、人机对照、样本量提示。
//
// Query 参数：
//   - start_date  开始日期
//   - end_date    结束日期
func (h *AnalyticsHandler) aiSpecialtyMetrics(c *gin.Context) {
	start, end := parseDateRange(c)

	metrics, err := h.svc.GetAISpecialtyMetrics(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": metrics,
	})
}

// aiFeatureBreakdown AI 功能使用明细（T5-8）。
//
// 按功能维度拆解 AI 各能力的使用情况。
//
// Query 参数：
//   - start_date  开始日期
//   - end_date    结束日期
func (h *AnalyticsHandler) aiFeatureBreakdown(c *gin.Context) {
	start, end := parseDateRange(c)

	breakdown, err := h.svc.GetAIFeatureBreakdown(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": breakdown,
	})
}

// ——— 工具函数 ———

// parseDateRange 解析日期范围参数。
// 没有传 start_date 则默认 30 天前，没有传 end_date 则默认今天。
func parseDateRange(c *gin.Context) (time.Time, time.Time) {
	now := time.Now()
	end := now
	start := now.AddDate(0, 0, -30) // 默认 30 天

	if s := c.Query("start_date"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			start = t
		}
	}
	if s := c.Query("end_date"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			end = t.Add(24 * time.Hour) // 包含当天
		}
	}

	return start, end
}

// getClientIP 获取客户端 IP。
func getClientIP(c *gin.Context) string {
	if ip := c.GetHeader("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := c.GetHeader("X-Real-IP"); ip != "" {
		return ip
	}
	return c.ClientIP()
}
