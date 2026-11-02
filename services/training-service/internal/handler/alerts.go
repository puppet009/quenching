// Package handler 告警规则 HTTP 接口（T1-23）。
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// AlertHandler 告警接口处理器。
type AlertHandler struct {
	manager *service.AlertManager
}

// NewAlertHandler 创建告警处理器。
func NewAlertHandler(mgr *service.AlertManager) *AlertHandler {
	return &AlertHandler{manager: mgr}
}

// RegisterRoutes 注册路由。
//
// GET    /api/v1/alerts/rules          列出告警规则
// POST   /api/v1/alerts/rules          创建告警规则
// GET    /api/v1/alerts/rules/:id      获取规则详情
// PUT    /api/v1/alerts/rules/:id      更新告警规则
// DELETE /api/v1/alerts/rules/:id      删除告警规则
// GET    /api/v1/alerts/list           列出告警实例
// POST   /api/v1/alerts/:id/ack        确认告警
// GET    /api/v1/alerts/summary        告警概览统计
func (h *AlertHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/alerts", UserID())
	{
		// 规则管理
		g.GET("/rules", h.listRules)
		g.POST("/rules", h.createRule)
		g.GET("/rules/:id", h.getRule)
		g.PUT("/rules/:id", h.updateRule)
		g.DELETE("/rules/:id", h.deleteRule)

		// 告警实例
		g.GET("/list", h.listAlerts)
		g.POST("/:id/ack", h.acknowledge)
		g.GET("/summary", h.summary)
	}
}

// listRules 列出告警规则。
//
// Query 参数：
//   - sandbox_id  沙箱ID（可选）
//   - enabled     是否只看启用的（可选）
func (h *AlertHandler) listRules(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	rules := h.manager.ListRules()

	// 过滤
	sandboxID := c.Query("sandbox_id")
	enabledOnly := c.Query("enabled") == "true"

	filtered := make([]*service.AlertRule, 0, len(rules))
	for _, r := range rules {
		if sandboxID != "" && r.SandboxID != sandboxID {
			continue
		}
		if enabledOnly && !r.Enabled {
			continue
		}
		filtered = append(filtered, r)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": filtered,
	})
}

// createRule 创建告警规则。
func (h *AlertHandler) createRule(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	var rule service.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "参数错误: " + err.Error()})
		return
	}

	if rule.ID == "" {
		rule.ID = "rule-" + time.Now().Format("20060102150405")
	}

	h.manager.AddRule(&rule)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": rule,
	})
}

// getRule 获取规则详情。
func (h *AlertHandler) getRule(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	id := c.Param("id")
	rule, exists := h.manager.GetRule(id)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "msg": "规则不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": rule,
	})
}

// updateRule 更新告警规则。
func (h *AlertHandler) updateRule(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	id := c.Param("id")
	_, exists := h.manager.GetRule(id)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "msg": "规则不存在"})
		return
	}

	var rule service.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "参数错误: " + err.Error()})
		return
	}

	rule.ID = id
	h.manager.AddRule(&rule)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": rule,
	})
}

// deleteRule 删除告警规则。
func (h *AlertHandler) deleteRule(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	id := c.Param("id")
	h.manager.RemoveRule(id)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
	})
}

// listAlerts 列出告警实例。
//
// Query 参数：
//   - sandbox_id  沙箱ID（可选）
//   - status      状态过滤（可选）：ok/pending/firing/acknowledged/resolved
func (h *AlertHandler) listAlerts(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	status := service.AlertStatus(c.Query("status"))
	sandboxID := c.Query("sandbox_id")

	alerts := h.manager.ListAlerts(status)

	// 按沙箱过滤
	if sandboxID != "" {
		filtered := make([]*service.AlertInstance, 0, len(alerts))
		for _, a := range alerts {
			if a.SandboxID == sandboxID {
				filtered = append(filtered, a)
			}
		}
		alerts = filtered
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": alerts,
	})
}

// acknowledge 确认告警。
func (h *AlertHandler) acknowledge(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	id := c.Param("id")
	success := h.manager.AcknowledgeAlert(id)
	if !success {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "msg": "告警不存在或无法确认"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
	})
}

// summary 告警概览统计。
//
// 返回各级别告警数量、状态分布等。
func (h *AlertHandler) summary(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	alerts := h.manager.ListAlerts("")

	summary := map[string]interface{}{
		"total":        0,
		"firing":       0,
		"pending":      0,
		"resolved":     0,
		"acknowledged": 0,
		"critical":     0,
		"warning":      0,
		"info":         0,
	}

	for _, a := range alerts {
		if sandboxID != "" && a.SandboxID != sandboxID {
			continue
		}

		summary["total"] = summary["total"].(int) + 1

		switch a.Status {
		case service.AlertStatusFiring:
			summary["firing"] = summary["firing"].(int) + 1
		case service.AlertStatusPending:
			summary["pending"] = summary["pending"].(int) + 1
		case service.AlertStatusResolved:
			summary["resolved"] = summary["resolved"].(int) + 1
		case service.AlertStatusAcknowledged:
			summary["acknowledged"] = summary["acknowledged"].(int) + 1
		}

		switch a.AlertSeverity {
		case service.AlertSeverityCritical:
			summary["critical"] = summary["critical"].(int) + 1
		case service.AlertSeverityWarning:
			summary["warning"] = summary["warning"].(int) + 1
		case service.AlertSeverityInfo:
			summary["info"] = summary["info"].(int) + 1
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": summary,
	})
}
