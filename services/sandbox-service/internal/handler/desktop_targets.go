// Package handler 桌面服务器目标接口（桌面化第一批）。
//
//	GET /api/v1/sandboxes/:id/targets  列出沙箱内可连接的服务器(Pod)
//
// 复用 UserID() 中间件（网关透传 X-User-Id）。
package handler

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"

	"github.com/quenching/sandbox-service/internal/service"
)

// DesktopHandler 桌面目标与终端处理器。
type DesktopHandler struct {
	Owned func(context.Context, int64, int64) error
	kc    *service.K8sClient
	// activity 活跃度续租信号（可为 nil：本地降级/单测）。
	audit    trainingAuditSink
	activity *service.ActivityTracker
}

// NewDesktopHandler 创建桌面处理器。
func NewDesktopHandler(kc *service.K8sClient, activity *service.ActivityTracker) *DesktopHandler {
	return &DesktopHandler{kc: kc, activity: activity}
}

// ListTargets 列出沙箱内全部服务器目标。
func (h *DesktopHandler) ListTargets(c *gin.Context) {
	sessionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessionID <= 0 {
		c.JSON(400, gin.H{"code": 10001, "msg": "会话ID非法"})
		return
	}

	targets, err := h.kc.ListTargets(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(500, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	response.OK(c, targets)
}
