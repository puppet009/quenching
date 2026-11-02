// Package handler 难度参数 HTTP 接口。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// DifficultyHandler 难度参数接口处理器。
type DifficultyHandler struct {
	svc *service.DifficultyService
}

// NewDifficultyHandler 创建难度参数处理器。
func NewDifficultyHandler(svc *service.DifficultyService) *DifficultyHandler {
	return &DifficultyHandler{svc: svc}
}

// RegisterRoutes 注册路由。
// GET /api/v1/difficulties       列出所有难度选项
// GET /api/v1/difficulties/:level 获取指定难度详情
func (h *DifficultyHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/difficulties", UserID())
	{
		g.GET("", h.list)
		g.GET("/:level", h.get)
	}
}

// list 返回所有可用难度列表（前端展示选项）。
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": [
//	    {"level":"easy","name":"简单","description":"告警清晰、日志完整...", ...},
//	    {"level":"normal","name":"普通", ...},
//	    {"level":"hard","name":"困难", ...}
//	  ]
//	}
func (h *DifficultyHandler) list(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	list := h.svc.ListDifficulties(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": list,
	})
}

// get 获取指定难度的完整参数配置。
//
// 路径参数：level - 难度等级 (easy/normal/hard)
func (h *DifficultyHandler) get(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	level := c.Param("level")
	cfg, err := h.svc.GetDifficulty(c.Request.Context(), level)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": cfg,
	})
}
