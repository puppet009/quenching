// Package handler 故障恢复验证 HTTP 接口（T1-29）。
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// RecoveryHandler 恢复验证处理器。
type RecoveryHandler struct {
	svc *service.RecoveryService
}

// NewRecoveryHandler 创建恢复验证处理器。
func NewRecoveryHandler(svc *service.RecoveryService) *RecoveryHandler {
	return &RecoveryHandler{svc: svc}
}

// RegisterRoutes 注册路由。
//
// POST   /api/v1/recovery/verify          执行恢复验证
// GET    /api/v1/recovery/result          获取最近一次验证结果
// GET    /api/v1/recovery/checks          列出所有检查项
// GET    /api/v1/recovery/history         验证历史（最近 N 次）
func (h *RecoveryHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/recovery", UserID())
	{
		g.POST("/verify", h.verify)
		g.GET("/result", h.getResult)
		g.GET("/checks", h.listChecks)
	}
}

// verify 执行一次恢复验证。
//
// 请求体：
//
//	{
//	  "sandbox_id": "sandbox-123",
//	  "session_id": "sess-abc"
//	}
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "session_id": "sess-abc",
//	    "sandbox_id": "sandbox-123",
//	    "overall_status": "failed",
//	    "score": 65.5,
//	    "total_checks": 10,
//	    "passed_count": 7,
//	    "failed_count": 3,
//	    "skipped_count": 0,
//	    "checks": [
//	      {
//	        "id": "metrics_user-service_health",
//	        "name": "user-service 指标健康",
//	        "category": "metrics",
//	        "status": "passed",
//	        "message": "user-service 指标正常",
//	        "weight": 10,
//	        "checked_at": 1726000000000
//	      },
//	      ...
//	    ],
//	    "start_time": 1726000000000,
//	    "end_time": 1726000002500,
//	    "duration_ms": 2500
//	  }
//	}
func (h *RecoveryHandler) verify(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	var req struct {
		SandboxID string `json:"sandbox_id" binding:"required"`
		SessionID string `json:"session_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "参数错误: " + err.Error()})
		return
	}

	result, err := h.svc.Verify(c.Request.Context(), req.SandboxID, req.SessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "恢复验证失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// getResult 获取最近一次验证结果。
//
// Query 参数：
//   - session_id  会话ID（必填）
func (h *RecoveryHandler) getResult(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sessionID := c.Query("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 session_id 参数"})
		return
	}

	result, exists := h.svc.GetLastResult(sessionID)
	if !exists {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ok",
			"data": nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// listChecks 列出所有检查项（不执行，只看配置）。
//
// 返回所有可用的检查项及其权重、类别等信息，用于前端展示检查列表。
func (h *RecoveryHandler) listChecks(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	// 执行一次空验证来获取检查项列表（有点 hack，但复用逻辑）
	// 实际生产中应该单独维护检查项配置
	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		sandboxID = "dummy"
	}

	// 只返回检查项的定义信息（不执行检查）
	// 这里返回一个模板结果，前端可以看到有哪些检查项
	template := map[string]interface{}{
		"categories": []map[string]interface{}{
			{
				"category": "metrics",
				"name":     "指标检查",
				"items": []map[string]interface{}{
					{"id": "metrics_user-service_health", "name": "user-service 指标健康", "weight": 10},
					{"id": "metrics_product-service_health", "name": "product-service 指标健康", "weight": 10},
					{"id": "metrics_order-service_health", "name": "order-service 指标健康", "weight": 10},
				},
			},
			{
				"category": "alerts",
				"name":     "告警检查",
				"items": []map[string]interface{}{
					{"id": "alerts_no_critical", "name": "无严重告警", "weight": 20},
					{"id": "alerts_no_warning", "name": "无警告告警", "weight": 10},
				},
			},
			{
				"category": "logs",
				"name":     "日志检查",
				"items": []map[string]interface{}{
					{"id": "logs_error_rate", "name": "错误日志数量", "weight": 10},
				},
			},
			{
				"category": "health",
				"name":     "健康检查",
				"items": []map[string]interface{}{
					{"id": "health_all_services", "name": "所有服务健康", "weight": 15},
				},
			},
			{
				"category": "probe",
				"name":     "接口探活",
				"items": []map[string]interface{}{
					{"id": "probe_user_login", "name": "用户登录接口", "weight": 5},
					{"id": "probe_product_list", "name": "商品列表接口", "weight": 5},
					{"id": "probe_order_create", "name": "订单创建接口", "weight": 5},
				},
			},
		},
		"total_weight": 100,
		"generated_at": time.Now().UnixMilli(),
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": template,
	})
}
