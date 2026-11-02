// Package handler 的 runtime.go：故障注入运行时接口（服务间内网调用）。
//
// 调用方：training-service（实训开始/结束、踩坑后果传导）。
// 约定：这些接口走内部网络，不挂管理后台鉴权中间件；错误码与全站统一
// （10001 参数/业务错误、10002 不存在）。
//
//	POST /api/v1/runtime/sessions/start        启动剧本自动编排
//	POST /api/v1/runtime/sessions/:id/stop      结束会话并补偿撤销
//	POST /api/v1/runtime/inject                 临时注入一次（次生故障）
//	POST /api/v1/runtime/revoke                 按 uid 撤销单次注入
//	GET  /api/v1/runtime/sessions/:id/experiments 查询会话注入历史
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/quenching/fault-engine/internal/repository"
	"github.com/quenching/fault-engine/internal/service"
)

// RuntimeHandler 运行时注入接口处理器。
type RuntimeHandler struct {
	svc   *service.RuntimeService
	token string
}

// NewRuntimeHandler 创建运行时处理器。
func NewRuntimeHandler(svc *service.RuntimeService, tokens ...string) *RuntimeHandler {
	token := ""
	if len(tokens) > 0 {
		token = tokens[0]
	}
	return &RuntimeHandler{svc: svc, token: token}
}

// RegisterRoutes 在给定引擎上注册运行时路由。
func (h *RuntimeHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/runtime", internalFaultGuard(h.token))
	{
		g.POST("/sessions/start", h.start)
		g.POST("/sessions/:id/stop", h.stop)
		g.POST("/inject", h.inject)
		g.POST("/revoke", h.revoke)
		g.GET("/sessions/:id/experiments", h.listExperiments)
	}
}

// start 启动剧本编排。
func (h *RuntimeHandler) start(c *gin.Context) {
	var req service.StartScenarioReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误: " + err.Error()})
		return
	}

	exp, err := h.svc.StartScenario(c.Request.Context(), req)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": exp})
}

// stop 结束会话并撤销全部活跃注入。
func (h *RuntimeHandler) stop(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "会话ID非法"})
		return
	}
	if err := h.svc.StopSession(c.Request.Context(), id); err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// inject 临时注入一次故障。
func (h *RuntimeHandler) inject(c *gin.Context) {
	var req service.InjectOnceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误: " + err.Error()})
		return
	}

	exp, err := h.svc.InjectOnce(c.Request.Context(), req)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": exp})
}

// revokeReq 撤销请求。
type revokeReq struct {
	SessionID int64  `json:"session_id"`
	UID       string `json:"uid"`
}

// revoke 按 blade uid 撤销单次注入。
func (h *RuntimeHandler) revoke(c *gin.Context) {
	var req revokeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误: " + err.Error()})
		return
	}
	if err := h.svc.RevokeOne(c.Request.Context(), req.SessionID, req.UID); err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// listExperiments 查询会话注入历史。
func (h *RuntimeHandler) listExperiments(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "会话ID非法"})
		return
	}
	items, err := h.svc.ListExperiments(c.Request.Context(), id)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}

// writeErr 统一映射领域错误到状态码/业务码。
func (h *RuntimeHandler) writeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrDefectNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "剧本不存在"})
	case errors.Is(err, repository.ErrExperimentNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "注入实验不存在"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
	}
}
