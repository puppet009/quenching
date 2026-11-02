// 服务端埋点上报内部接口（/internal/v1）。
//
// 供 user-service / training-service / sandbox-service 在关键节点直连上报，
// 东西向流量不经网关。配置 token 后校验 X-Internal-Token；本地开发留空不校验。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/quenching/review-service/internal/service"
)

// InternalAnalyticsHandler 服务端埋点上报处理器。
type InternalAnalyticsHandler struct {
	svc   *service.AnalyticsService
	token string
}

// NewInternalAnalyticsHandler 创建服务端埋点内部接口处理器。
func NewInternalAnalyticsHandler(svc *service.AnalyticsService, token string) *InternalAnalyticsHandler {
	return &InternalAnalyticsHandler{svc: svc, token: token}
}

// RegisterRoutes 注册内部埋点路由。
//
//	POST /internal/v1/analytics/events
func (h *InternalAnalyticsHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/analytics", h.guard)
	{
		g.POST("/events", h.track)
	}
}

// guard 内部接口共享头校验。
func (h *InternalAnalyticsHandler) guard(c *gin.Context) {
	if h.token != "" && c.GetHeader("X-Internal-Token") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 14001, "msg": "内部接口未授权"})
		return
	}
	c.Next()
}

// track 接收服务端事件；唯一事件重复上报返回 recorded=false，不报错。
func (h *InternalAnalyticsHandler) track(c *gin.Context) {
	var req service.TrackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}

	req.Platform = c.GetHeader("X-Platform")

	recorded, err := h.svc.Track(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "recorded": recorded})
}
