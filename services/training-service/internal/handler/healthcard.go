// Package handler 服务健康卡片 HTTP 接口（T1-20）。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// HealthCardHandler 健康卡片处理器。
type HealthCardHandler struct {
	svc *service.HealthCardService
}

// NewHealthCardHandler 创建健康卡片处理器。
func NewHealthCardHandler(svc *service.HealthCardService) *HealthCardHandler {
	return &HealthCardHandler{svc: svc}
}

// RegisterRoutes 注册路由。
//
// GET /api/v1/health/overview   健康概览（所有服务卡片）
// GET /api/v1/health/service/:service  单个服务健康详情
// GET /api/v1/health/summary    健康统计摘要
// POST /api/v1/health/refresh   强制刷新健康数据
func (h *HealthCardHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/health", UserID())
	{
		g.GET("/overview", h.overview)
		g.GET("/service/:service", h.serviceDetail)
		g.GET("/summary", h.summary)
		g.POST("/refresh", h.refresh)
	}
}

// overview 健康概览（所有服务的健康卡片）。
//
// Query 参数：
//   - sandbox_id  沙箱ID（必填）
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "sandbox_id": "sandbox-123",
//	    "total_services": 3,
//	    "healthy_count": 1,
//	    "warning_count": 1,
//	    "critical_count": 1,
//	    "unknown_count": 0,
//	    "total_alerts": 5,
//	    "services": [
//	      {
//	        "service": "order-service",
//	        "status": "critical",
//	        "cpu_usage": 95.5,
//	        "memory_usage": 72.3,
//	        "qps": 45.2,
//	        "error_rate": 8.5,
//	        "p99_latency": 0.85,
//	        "alert_count": 3,
//	        "critical_count": 2,
//	        "warning_count": 1,
//	        "reason": "CPU 使用率过高",
//	        "updated_at": 1726000000000
//	      },
//	      ...
//	    ],
//	    "updated_at": 1726000000000
//	  }
//	}
func (h *HealthCardHandler) overview(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	result, err := h.svc.GetOverview(c.Request.Context(), sandboxID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "获取健康概览失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// serviceDetail 单个服务的健康详情。
//
// Path 参数：
//   - service  服务名
//
// Query 参数：
//   - sandbox_id  沙箱ID（必填）
func (h *HealthCardHandler) serviceDetail(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	svcName := c.Param("service")
	result, err := h.svc.GetServiceCard(c.Request.Context(), sandboxID, svcName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// summary 健康统计摘要。
//
// 返回精简的统计数据，用于顶部状态栏或小程序卡片。
//
// Query 参数：
//   - sandbox_id  沙箱ID（必填）
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "total_services": 3,
//	    "healthy_count": 1,
//	    "warning_count": 1,
//	    "critical_count": 1,
//	    "total_alerts": 5,
//	    "overall_status": "critical",
//	    "updated_at": 1726000000000
//	  }
//	}
func (h *HealthCardHandler) summary(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	overview, err := h.svc.GetOverview(c.Request.Context(), sandboxID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "获取健康摘要失败"})
		return
	}

	// 计算整体状态
	overallStatus := "healthy"
	if overview.CriticalCount > 0 {
		overallStatus = "critical"
	} else if overview.WarningCount > 0 {
		overallStatus = "warning"
	} else if overview.UnknownCount > 0 && overview.HealthyCount == 0 {
		overallStatus = "unknown"
	}

	summary := map[string]interface{}{
		"total_services": overview.TotalServices,
		"healthy_count":  overview.HealthyCount,
		"warning_count":  overview.WarningCount,
		"critical_count": overview.CriticalCount,
		"total_alerts":   overview.TotalAlerts,
		"overall_status": overallStatus,
		"updated_at":     overview.UpdatedAt,
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": summary,
	})
}

// refresh 强制刷新健康数据。
//
// 清除缓存，重新从 Prometheus 和告警引擎拉取数据。
//
// Query 参数：
//   - sandbox_id  沙箱ID（必填）
func (h *HealthCardHandler) refresh(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	result, err := h.svc.Refresh(c.Request.Context(), sandboxID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "刷新健康数据失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}
