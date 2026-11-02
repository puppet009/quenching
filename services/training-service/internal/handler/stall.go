// Package handler 训练会话卡顿状态查询接口（T3-6）。
//
// 卡顿状态由后台扫描器周期性评估并保存在内存中，本处理器只做只读查询，
// 不触发评估、不写库、不推送（推送属 T3-7）。
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// StallHandler 卡顿状态查询处理器。
type StallHandler struct {
	mon *service.StallMonitor
}

// NewStallHandler 创建卡顿状态查询处理器。
func NewStallHandler(mon *service.StallMonitor) *StallHandler {
	return &StallHandler{mon: mon}
}

// RegisterRoutes 注册路由。
//
//	GET /api/v1/stall/status?session_id=123  单会话卡顿状态
//	GET /api/v1/stall/list                   全部 running 会话卡顿状态（管理/验证用）
func (h *StallHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/stall", UserID())
	{
		g.GET("/status", h.status)
		g.GET("/list", h.list)
	}
}

// status 查询单会话卡顿状态。
//
// 状态来自内存扫描结果：会话不存在 / 已结束（被清理）/ 首轮扫描尚未覆盖时返回 40401。
func (h *StallHandler) status(c *gin.Context) {
	if _, ok := MustUserID(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	raw := c.Query("session_id")
	sessionID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || sessionID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少或非法的 session_id 参数"})
		return
	}

	st, ok := h.mon.Status(sessionID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 40401, "msg": "会话不存在或不在 running 状态，暂无卡顿评估结果"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": st,
	})
}

// list 返回全部已扫描 running 会话的卡顿状态（按会话 ID 升序）。
func (h *StallHandler) list(c *gin.Context) {
	if _, ok := MustUserID(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	items := h.mon.ListStatuses()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"total": len(items),
			"items": items,
		},
	})
}
