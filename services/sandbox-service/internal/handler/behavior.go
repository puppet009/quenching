// Package handler 行为模拟管理接口（行为模拟第一批）。
//
//	POST /api/v1/sandboxes/:id/behavior/start  启动行为模拟
//	POST /api/v1/sandboxes/:id/behavior/stop   停止行为模拟
//	GET  /api/v1/sandboxes/:id/behavior/status 查询运行状态
//
// 这些接口供管理与排障使用；沙箱正常生命周期内行为模拟由
// SandboxService 在 ready 后自动启动，MVP 前端不暴露操作按钮。
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/quenching/sandbox-service/internal/service"
)

// BehaviorHandler 行为模拟管理处理器。
type BehaviorHandler struct {
	runtime *service.BehaviorRuntime
}

// NewBehaviorHandler 创建行为模拟处理器。
func NewBehaviorHandler(runtime *service.BehaviorRuntime) *BehaviorHandler {
	return &BehaviorHandler{runtime: runtime}
}

// RegisterBehavior 在沙箱路由组内注册行为模拟路由。
func (h *BehaviorHandler) RegisterBehavior(g *gin.RouterGroup) {
	bg := g.Group("/:id/behavior")
	bg.POST("/start", h.Start)
	bg.POST("/stop", h.Stop)
	bg.GET("/status", h.Status)
}

// behaviorStartReq 启动请求体（全部可选）。
type behaviorStartReq struct {
	EntryURL       string `json:"entry_url"`
	SignalEndpoint string `json:"signal_endpoint"`
	Image          string `json:"image"`
}

// Start 启动行为模拟。
func (h *BehaviorHandler) Start(c *gin.Context) {
	sessionID, ok := parseBehaviorSessionID(c)
	if !ok {
		return
	}
	req := behaviorStartReq{}
	// 请求体允许为空（全部字段可选）；绑定失败仅在显式传入非法 JSON 时报错。
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"code": 10001, "msg": "请求体格式错误"})
			return
		}
	}

	st, err := h.runtime.Start(c.Request.Context(), service.BehaviorStartConfig{
		SessionID:      sessionID,
		EntryURL:       req.EntryURL,
		SignalEndpoint: req.SignalEndpoint,
		Image:          req.Image,
	})
	if err != nil {
		c.JSON(500, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok", "data": st})
}

// Stop 停止行为模拟。
func (h *BehaviorHandler) Stop(c *gin.Context) {
	sessionID, ok := parseBehaviorSessionID(c)
	if !ok {
		return
	}
	if err := h.runtime.Stop(c.Request.Context(), sessionID); err != nil {
		c.JSON(500, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

// Status 查询行为模拟状态。
func (h *BehaviorHandler) Status(c *gin.Context) {
	sessionID, ok := parseBehaviorSessionID(c)
	if !ok {
		return
	}
	st, err := h.runtime.Status(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(500, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok", "data": st})
}

// parseBehaviorSessionID 从路径解析会话ID；失败时已写响应，返回 false。
func parseBehaviorSessionID(c *gin.Context) (int64, bool) {
	sessionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessionID <= 0 {
		c.JSON(400, gin.H{"code": 10001, "msg": "会话ID非法"})
		return 0, false
	}
	return sessionID, true
}
