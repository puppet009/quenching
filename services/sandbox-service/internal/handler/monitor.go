package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// MonitorHandler 沙箱监控管理后台接口。
//
// 接口清单：
//
//	GET    /api/admin/v1/sandboxes              沙箱列表（分页+筛选）
//	GET    /api/admin/v1/sandboxes/stats        沙箱统计概览
//	GET    /api/admin/v1/sandboxes/:session_id  沙箱详情（含运行时长/指标）
//	DELETE /api/admin/v1/sandboxes/:session_id  手动释放沙箱
//	GET    /api/admin/v1/sandboxes/:session_id/metrics     最新资源指标
//	GET    /api/admin/v1/sandboxes/:session_id/metrics/history  指标历史
//	GET    /api/admin/v1/anomalies              异常告警列表
//	PATCH  /api/admin/v1/anomalies/:id/resolve  标记异常已解决
type MonitorHandler struct {
	svc *service.MonitorService
}

// NewMonitorHandler 创建监控处理器。
func NewMonitorHandler(svc *service.MonitorService) *MonitorHandler {
	return &MonitorHandler{svc: svc}
}

// RegisterRoutes 注册管理后台路由。
func (h *MonitorHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/admin/v1")
	{
		// 沙箱管理
		sb := g.Group("/sandboxes")
		{
			sb.GET("", h.listSandboxes)
			sb.GET("/stats", h.getStats)
			sb.GET("/:session_id", h.getSandboxDetail)
			sb.DELETE("/:session_id", h.releaseSandbox)
			sb.GET("/:session_id/metrics", h.getMetrics)
			sb.GET("/:session_id/metrics/history", h.getMetricsHistory)
		}

		// 异常告警
		an := g.Group("/anomalies")
		{
			an.GET("", h.listAnomalies)
			an.PATCH("/:id/resolve", h.resolveAnomaly)
		}
	}
}

// ——— 沙箱列表 ———

// listSandboxes 分页查询沙箱列表。
//
// Query 参数：
//   - user_id     按用户筛选
//   - project_id  按项目筛选
//   - status      按状态筛选
//   - namespace   命名空间搜索
//   - is_active   是否活跃（true/false，不传=全部）
//   - page        页码
//   - page_size   每页条数
func (h *MonitorHandler) listSandboxes(c *gin.Context) {
	q := model.SandboxQuery{
		Page:     parseInt(c.Query("page"), 1),
		PageSize: parseInt(c.Query("page_size"), 10),
	}

	if v := c.Query("user_id"); v != "" {
		q.UserID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("project_id"); v != "" {
		q.ProjectID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("status"); v != "" {
		q.Status = v
	}
	if v := c.Query("namespace"); v != "" {
		q.Namespace = v
	}
	if v := c.Query("is_active"); v != "" {
		active := v == "true"
		q.IsActive = &active
	}

	result, err := h.svc.ListSandboxes(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, result)
}

// getStats 沙箱统计概览。
func (h *MonitorHandler) getStats(c *gin.Context) {
	stats, err := h.svc.GetSandboxStats(c.Request.Context())
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, stats)
}

// getSandboxDetail 沙箱详情。
func (h *MonitorHandler) getSandboxDetail(c *gin.Context) {
	sessionID, err := parseSessionID(c)
	if err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	detail, err := h.svc.GetSandboxDetail(c.Request.Context(), sessionID)
	if err != nil {
		if errors.Is(err, service.ErrSandboxNotFound.Unwrap()) {
			response.Fail(c, service.ErrSandboxNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, detail)
}

// releaseSandbox 手动释放沙箱。
func (h *MonitorHandler) releaseSandbox(c *gin.Context) {
	sessionID, err := parseSessionID(c)
	if err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	// 可选：释放原因
	reason := c.Query("reason")

	if err := h.svc.ReleaseSandbox(c.Request.Context(), sessionID, reason); err != nil {
		response.Fail(c, service.ErrSandboxConflict)
		return
	}
	response.OK(c, gin.H{"releasing": true, "session_id": sessionID})
}

// ——— 资源监控 ———

// getMetrics 获取沙箱最新资源指标。
func (h *MonitorHandler) getMetrics(c *gin.Context) {
	sessionID, err := parseSessionID(c)
	if err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	metrics, err := h.svc.GetLatestMetrics(c.Request.Context(), sessionID)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	if metrics == nil {
		response.OK(c, nil)
		return
	}
	response.OK(c, metrics)
}

// getMetricsHistory 获取指标历史（最近 N 条）。
func (h *MonitorHandler) getMetricsHistory(c *gin.Context) {
	sessionID, err := parseSessionID(c)
	if err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	limit := parseInt(c.Query("limit"), 30)

	history, err := h.svc.GetMetricsHistory(c.Request.Context(), sessionID, limit)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, gin.H{
		"items": history,
		"total": len(history),
	})
}

// ——— 异常告警 ———

// listAnomalies 异常告警列表。
//
// Query 参数：
//   - level         级别（warning/error）
//   - anomaly_type  类型
//   - resolved      是否已解决（true/false，不传=全部）
//   - page          页码
//   - page_size     每页条数
func (h *MonitorHandler) listAnomalies(c *gin.Context) {
	q := model.AnomalyQuery{
		Page:     parseInt(c.Query("page"), 1),
		PageSize: parseInt(c.Query("page_size"), 10),
	}

	if v := c.Query("level"); v != "" {
		q.Level = model.AnomalyLevel(v)
	}
	if v := c.Query("anomaly_type"); v != "" {
		q.AnomalyType = v
	}
	if v := c.Query("resolved"); v != "" {
		resolved := v == "true"
		q.Resolved = &resolved
	}

	result, err := h.svc.ListAnomalies(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, result)
}

// resolveAnomaly 标记异常已解决。
func (h *MonitorHandler) resolveAnomaly(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	if err := h.svc.ResolveAnomaly(c.Request.Context(), id); err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}
	response.OK(c, gin.H{"resolved": true})
}

// ——— 工具函数 ———

func parseSessionID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("session_id"), 10, 64)
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// 兼容 gin.H 导入（避免未使用报错）
var _ = http.StatusOK
