// 服务间复盘聚合接口处理器（/internal/v1，架构拆库第五批）。
//
// 路由仅供 review-service 直连调用，不经 api-gateway、不对前端暴露。
// 安全上做共享头简单校验（X-Internal-Token），云上再叠加网络策略做来源隔离；
// token 留空时不校验（兼容本地无密钥开发）。
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// InternalReviewInputHandler 复盘聚合内部接口处理器。
type InternalReviewInputHandler struct {
	svc   *service.InternalReviewInputService
	token string
}

// NewInternalReviewInputHandler 创建复盘聚合内部接口处理器。
func NewInternalReviewInputHandler(svc *service.InternalReviewInputService, token string) *InternalReviewInputHandler {
	return &InternalReviewInputHandler{svc: svc, token: token}
}

// RegisterRoutes 注册服务间路由：
//
//	GET /internal/v1/sessions/:id/review-input 复盘主输入（难度+时间线+剧本）
//	GET /internal/v1/sessions/:id/pitfalls     AI 坑点原始数据
//	GET /internal/v1/sessions/:id/changes      助手文件修改记录（M5）
func (h *InternalReviewInputHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/sessions", h.guard)
	g.GET("/:id/review-input", h.getReviewInput)
	g.GET("/:id/pitfalls", h.listPitfalls)
	g.GET("/:id/changes", h.listChanges)
}

// guard 内部接口共享头校验：配置了 token 才校验，留空放行。
func (h *InternalReviewInputHandler) guard(c *gin.Context) {
	if h.token != "" && c.GetHeader("X-Internal-Token") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 14001, "msg": "内部接口未授权"})
		return
	}
	c.Next()
}

// parseSessionID 解析路径会话 ID。
func parseSessionID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "会话ID非法"})
		return 0, false
	}
	return id, true
}

// getReviewInput 返回复盘主输入聚合。
func (h *InternalReviewInputHandler) getReviewInput(c *gin.Context) {
	sessionID, ok := parseSessionID(c)
	if !ok {
		return
	}
	in, err := h.svc.GetReviewInput(c.Request.Context(), sessionID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": in})
}

// listPitfalls 返回会话 AI 坑点原始数据。
func (h *InternalReviewInputHandler) listPitfalls(c *gin.Context) {
	sessionID, ok := parseSessionID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListPitfalls(c.Request.Context(), sessionID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}

// listChanges 返回助手文件修改记录。
func (h *InternalReviewInputHandler) listChanges(c *gin.Context) {
	sessionID, ok := parseSessionID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListChanges(c.Request.Context(), sessionID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}

// writeError 按错误类型映射状态码与业务码。
func (h *InternalReviewInputHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInternalReviewSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": 15001, "msg": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
	}
}
